package session

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// codexAdapter reads Codex CLI rollouts: date-partitioned JSONL files whose
// first line is a session_meta header. Subagents are identified by the
// header's source marker. The grammar has no tool error flag except the Ok/Err
// union on MCP call results, so most tool events carry an unknown error state.
type codexAdapter struct{}

type codexEnvelope struct {
	Payload   json.RawMessage `json:"payload"`
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
}

func (codexAdapter) discover(root string, _ bool) ([]transcript, []Diagnostic) {
	return walkTranscripts(root, walkRules{
		classify: func(path string) (bool, bool) {
			return filepath.Ext(path) == ".jsonl", false
		},
	})
}

func (codexAdapter) meta(t transcript) (Session, []Diagnostic) {
	entry := Session{
		Harness:    HarnessCodex,
		ID:         codexFallbackID(t.path),
		ModifiedAt: t.modTime,
		Path:       t.path,
		Subagent:   t.subagent,
	}
	err := forEachLine(t.path, func(_ int, data []byte) bool {
		var envelope codexEnvelope
		if json.Unmarshal(data, &envelope) != nil || envelope.Type != "session_meta" {
			return false
		}
		entry.StartedAt = parseTimestamp(envelope.Timestamp)
		var payload struct {
			Cwd       string          `json:"cwd"`
			ID        string          `json:"id"`
			Source    json.RawMessage `json:"source"`
			Timestamp string          `json:"timestamp"`
		}
		if json.Unmarshal(envelope.Payload, &payload) != nil {
			return false
		}
		if payload.ID != "" {
			entry.ID = payload.ID
		}
		entry.Project = payload.Cwd
		entry.Subagent = entry.Subagent || codexSubagentSource(payload.Source)
		if entry.StartedAt.IsZero() {
			entry.StartedAt = parseTimestamp(payload.Timestamp)
		}
		return false
	})
	if err != nil {
		return entry, []Diagnostic{{Code: codeTranscriptRead, Harness: HarnessCodex, Message: err.Error(), Path: t.path}}
	}
	return entry, nil
}

func codexSubagentSource(raw json.RawMessage) bool {
	var source map[string]json.RawMessage
	if json.Unmarshal(raw, &source) != nil {
		return false
	}
	_, found := source["subagent"]
	return found
}

// codexFallbackID derives the session UUID from a rollout filename
// (rollout-<local timestamp>-<uuid>.jsonl) for headerless legacy files.
func codexFallbackID(path string) string {
	stem := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	rest, found := strings.CutPrefix(stem, "rollout-")
	if timestampWidth := len("2006-01-02T15-04-05-"); found && len(rest) > timestampWidth {
		return rest[timestampWidth:]
	}
	return stem
}

func (codexAdapter) scan(path string, visit func(event)) (int, error) {
	malformed := 0
	provenanceUserMessages := false
	toolNames := make(map[string]string)
	err := forEachLine(path, func(line int, data []byte) bool {
		var envelope codexEnvelope
		if json.Unmarshal(data, &envelope) != nil {
			malformed++
			return true
		}
		base := event{line: line, timestamp: parseTimestamp(envelope.Timestamp)}
		switch envelope.Type {
		case "response_item":
			provenanceUserMessages = codexResponseEvents(envelope.Payload, base, toolNames, visit) || provenanceUserMessages
		case "event_msg":
			codexEventMessage(envelope.Payload, base, provenanceUserMessages, visit)
		}
		return true
	})
	return malformed, err
}

// codexResponseEvents emits messages and tool calls from the response_item
// stream and reports whether it found provenance-tagged user content.
func codexResponseEvents(raw json.RawMessage, base event, toolNames map[string]string, visit func(event)) bool {
	var payload struct {
		Arguments string `json:"arguments"`
		CallID    string `json:"call_id"`
		Content   []struct {
			Text string `json:"text"`
		} `json:"content"`
		Metadata struct {
			ContentItemKinds []string `json:"content_item_kinds"`
		} `json:"internal_chat_message_metadata_passthrough"`
		Input  string          `json:"input"`
		Name   string          `json:"name"`
		Output json.RawMessage `json:"output"`
		Role   string          `json:"role"`
		Type   string          `json:"type"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return false
	}
	switch payload.Type {
	case "message":
		parts := make([]string, 0, len(payload.Content))
		for index, item := range payload.Content {
			if payload.Role == "user" && (index >= len(payload.Metadata.ContentItemKinds) || payload.Metadata.ContentItemKinds[index] != "user.text") {
				continue
			}
			if item.Text != "" {
				parts = append(parts, item.Text)
			}
		}
		text := strings.Join(parts, "\n")
		switch payload.Role {
		case "user":
			if text != "" {
				base.role, base.text = RoleUser, text
				visit(base)
			}
			return len(payload.Metadata.ContentItemKinds) > 0
		case "assistant":
			if text != "" {
				base.role, base.text = RoleAssistant, text
				visit(base)
			}
		}
	case "function_call", "custom_tool_call":
		toolNames[payload.CallID] = payload.Name
		base.role, base.tool = RoleTool, payload.Name
		base.text = payload.Arguments
		if base.text == "" {
			base.text = payload.Input
		}
		visit(base)
	case "function_call_output", "custom_tool_call_output":
		base.role, base.tool, base.text = RoleTool, toolNames[payload.CallID], flattenText(payload.Output)
		visit(base)
	}
	return false
}

// codexEventMessage emits legacy user messages and extracts the one tool error
// signal the grammar has: the Ok/Err union on mcp_tool_call_end results.
func codexEventMessage(raw json.RawMessage, base event, provenanceUserMessages bool, visit func(event)) {
	var payload struct {
		Invocation struct {
			Tool string `json:"tool"`
		} `json:"invocation"`
		Message string          `json:"message"`
		Result  json.RawMessage `json:"result"`
		Type    string          `json:"type"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return
	}
	switch payload.Type {
	case "user_message":
		if !provenanceUserMessages && payload.Message != "" {
			base.role, base.text = RoleUser, payload.Message
			visit(base)
		}
	case "mcp_tool_call_end":
		var result map[string]json.RawMessage
		if json.Unmarshal(payload.Result, &result) != nil {
			return
		}
		base.role, base.tool, base.text = RoleTool, payload.Invocation.Tool, compactJSON(payload.Result)
		switch {
		case result["Err"] != nil:
			base.err = errorYes
		case result["Ok"] != nil:
			base.err = errorNo
		default:
			base.err = errorUnknown
		}
		visit(base)
	}
}
