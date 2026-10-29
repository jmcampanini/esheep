package session

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strings"
)

// claudeAdapter reads Claude Code transcripts: one JSONL file per session
// under a per-project directory, with subagent transcripts in nested
// "subagents" directories. The grammar has no header line; session metadata
// repeats on conversational records, and one assistant API response spans
// several lines (one per content block).
type claudeAdapter struct{}

func (claudeAdapter) discover(root string, includeSubagents bool) ([]transcript, []Diagnostic) {
	return walkJSONLTranscripts(root, walkRules{
		isSubagent: func(path string) bool {
			return filepath.Base(filepath.Dir(path)) == "subagents"
		},
		skipDir: func(_ string, entry fs.DirEntry) bool {
			return !includeSubagents && entry.Name() == "subagents"
		},
	})
}

// claudeMetaLimit bounds the metadata prescan. Bookkeeping records cluster at
// the top of a transcript; the first conversational record carries cwd and
// timestamp, and ai-title usually lands early.
const claudeMetaLimit = 512

func (claudeAdapter) meta(t transcript, ids idFilter) describedSession {
	entry := Session{
		Harness:    HarnessClaude,
		ID:         strings.TrimSuffix(filepath.Base(t.path), ".jsonl"),
		ModifiedAt: t.modTime,
		Origin:     transcriptOrigin(t.subagent),
		Path:       t.path,
		Subagent:   t.subagent,
	}
	if !ids.matches(entry.ID) {
		return describedSession{session: entry}
	}

	err := forEachLine(t.path, func(line int, data []byte) bool {
		var record struct {
			AiTitle   string `json:"aiTitle"`
			Cwd       string `json:"cwd"`
			Timestamp string `json:"timestamp"`
		}
		if json.Unmarshal(data, &record) != nil {
			return line < claudeMetaLimit
		}
		if len(entry.Projects) == 0 {
			entry.Projects = projectPaths(record.Cwd)
		}
		if entry.StartedAt.IsZero() {
			entry.StartedAt = parseTimestamp(record.Timestamp)
		}
		if entry.Title == "" {
			entry.Title = record.AiTitle
		}
		return line < claudeMetaLimit && (len(entry.Projects) == 0 || entry.StartedAt.IsZero() || entry.Title == "")
	})
	return describedSession{eligible: true, err: err, session: entry}
}

func (claudeAdapter) scan(path string, visit func(event) bool) (int, error) {
	malformed := 0
	toolNames := make(map[string]string)
	err := forEachLine(path, func(line int, data []byte) bool {
		var envelope struct {
			claudeOriginMetadata
			IsMeta    bool            `json:"isMeta"`
			Message   json.RawMessage `json:"message"`
			Timestamp string          `json:"timestamp"`
			Type      string          `json:"type"`
		}
		if json.Unmarshal(data, &envelope) != nil {
			malformed++
			return true
		}
		base := event{line: line, timestamp: parseTimestamp(envelope.Timestamp)}
		switch envelope.Type {
		case "user":
			base.origin = envelope.eventOrigin()
			return claudeUserEvents(envelope.Message, envelope.IsMeta, base, toolNames, visit)
		case "assistant":
			return claudeAssistantEvents(envelope.Message, base, toolNames, visit)
		}
		return true
	})
	return malformed, err
}

type claudeContentItem struct {
	Content   json.RawMessage `json:"content"`
	ID        string          `json:"id"`
	Input     json.RawMessage `json:"input"`
	IsError   *bool           `json:"is_error"`
	Name      string          `json:"name"`
	Text      string          `json:"text"`
	ToolUseID string          `json:"tool_use_id"`
	Type      string          `json:"type"`
}

// claudeUserEvents emits user text and tool results. A user record's content
// is either the typed prompt string or an array mixing text and tool_result
// blocks; isMeta marks injected text the human never wrote.
func claudeUserEvents(message json.RawMessage, isMeta bool, base event, toolNames map[string]string, visit func(event) bool) bool {
	base.kind = KindMessage
	var wrapper struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(message, &wrapper) != nil {
		return true
	}
	var text string
	if json.Unmarshal(wrapper.Content, &text) == nil {
		if !isMeta && text != "" {
			base.role, base.text = RoleUser, text
			return visit(base)
		}
		return true
	}
	var items []claudeContentItem
	if json.Unmarshal(wrapper.Content, &items) != nil {
		return true
	}
	for _, item := range items {
		switch item.Type {
		case "text":
			if !isMeta && item.Text != "" {
				e := base
				e.role, e.text = RoleUser, item.Text
				if !visit(e) {
					return false
				}
			}
		case "tool_result":
			e := base
			e.callID, e.kind, e.origin = item.ToolUseID, KindToolResult, EventOrigin{Kind: EventOriginTool}
			e.role = RoleTool
			if item.ToolUseID != "" {
				e.tool = toolNames[item.ToolUseID]
			}
			e.text = flattenText(item.Content)
			e.failed = item.IsError != nil && *item.IsError
			if !visit(e) {
				return false
			}
		}
	}
	return true
}

func claudeAssistantEvents(message json.RawMessage, base event, toolNames map[string]string, visit func(event) bool) bool {
	base.kind = KindMessage
	base.origin = EventOrigin{Kind: EventOriginAgent}
	var wrapper struct {
		Content []claudeContentItem `json:"content"`
	}
	if json.Unmarshal(message, &wrapper) != nil {
		return true
	}
	for _, item := range wrapper.Content {
		switch item.Type {
		case "text":
			if item.Text != "" {
				e := base
				e.role, e.text = RoleAssistant, item.Text
				if !visit(e) {
					return false
				}
			}
		case "tool_use":
			if item.ID != "" {
				toolNames[item.ID] = item.Name
			}
			e := base
			e.callID, e.kind = item.ID, KindToolCall
			e.role, e.tool, e.text = RoleTool, item.Name, compactJSON(item.Input)
			if !visit(e) {
				return false
			}
		}
	}
	return true
}

type claudeOriginMetadata struct {
	Origin       json.RawMessage `json:"origin"`
	PromptSource json.RawMessage `json:"promptSource"`
	TurnOrigin   json.RawMessage `json:"turnOrigin"`
}

func (m claudeOriginMetadata) eventOrigin() EventOrigin {
	var origin map[string]json.RawMessage
	_ = json.Unmarshal(m.Origin, &origin)
	evidence := make(map[string]string)
	if value, ok := scalarEvidence(origin["kind"]); ok {
		evidence["origin.kind"] = value
	}
	appendOriginEvidence(evidence, "promptSource", m.PromptSource, 0)
	appendOriginEvidence(evidence, "turnOrigin", m.TurnOrigin, 0)

	var kind, turn string
	_ = json.Unmarshal(origin["kind"], &kind)
	_ = json.Unmarshal(m.TurnOrigin, &turn)
	mapped := claudeRecordedOrigin(kind)
	turnMapped := EventOriginUnknown
	switch turn {
	case "human":
		turnMapped = EventOriginHuman
	case "peer":
		turnMapped = EventOriginAgent
	case "task_notification":
		turnMapped = EventOriginHarness
	}
	if len(m.Origin) == 0 || bytes.Equal(bytes.TrimSpace(m.Origin), []byte("null")) {
		mapped = turnMapped
	} else if turn != "" && turnMapped != mapped {
		mapped = EventOriginUnknown
	}
	return EventOrigin{Evidence: evidence, Kind: mapped}
}

func claudeRecordedOrigin(kind string) EventOriginKind {
	switch kind {
	case "human":
		return EventOriginHuman
	case "task-notification":
		return EventOriginHarness
	case "peer", "coordinator":
		return EventOriginAgent
	default:
		return EventOriginUnknown
	}
}
