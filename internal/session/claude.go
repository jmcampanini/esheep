package session

import (
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
		skipDir: func(entry fs.DirEntry) bool {
			return !includeSubagents && entry.Name() == "subagents"
		},
	})
}

// claudeMetaLimit bounds the metadata prescan. Bookkeeping records cluster at
// the top of a transcript; the first conversational record carries cwd and
// timestamp, and ai-title usually lands early.
const claudeMetaLimit = 512

func (claudeAdapter) meta(t transcript) (Session, error) {
	entry := Session{
		Harness:    HarnessClaude,
		ID:         strings.TrimSuffix(filepath.Base(t.path), ".jsonl"),
		ModifiedAt: t.modTime,
		Path:       t.path,
		Subagent:   t.subagent,
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
		if entry.Project == "" {
			entry.Project = record.Cwd
		}
		if entry.StartedAt.IsZero() {
			entry.StartedAt = parseTimestamp(record.Timestamp)
		}
		if entry.Title == "" {
			entry.Title = record.AiTitle
		}
		return line < claudeMetaLimit && (entry.Project == "" || entry.StartedAt.IsZero() || entry.Title == "")
	})
	return entry, err
}

func (claudeAdapter) scan(path string, visit func(event)) (int, error) {
	malformed := 0
	toolNames := make(map[string]string)
	err := forEachLine(path, func(line int, data []byte) bool {
		var envelope struct {
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
			claudeUserEvents(envelope.Message, envelope.IsMeta, base, toolNames, visit)
		case "assistant":
			claudeAssistantEvents(envelope.Message, base, toolNames, visit)
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
func claudeUserEvents(message json.RawMessage, isMeta bool, base event, toolNames map[string]string, visit func(event)) {
	var wrapper struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(message, &wrapper) != nil {
		return
	}
	var text string
	if json.Unmarshal(wrapper.Content, &text) == nil {
		if !isMeta && text != "" {
			base.role, base.text = RoleUser, text
			visit(base)
		}
		return
	}
	var items []claudeContentItem
	if json.Unmarshal(wrapper.Content, &items) != nil {
		return
	}
	for _, item := range items {
		switch item.Type {
		case "text":
			if !isMeta && item.Text != "" {
				e := base
				e.role, e.text = RoleUser, item.Text
				visit(e)
			}
		case "tool_result":
			e := base
			e.role = RoleTool
			e.tool = toolNames[item.ToolUseID]
			e.text = flattenText(item.Content)
			e.failed = item.IsError != nil && *item.IsError
			visit(e)
		}
	}
}

func claudeAssistantEvents(message json.RawMessage, base event, toolNames map[string]string, visit func(event)) {
	var wrapper struct {
		Content []claudeContentItem `json:"content"`
	}
	if json.Unmarshal(message, &wrapper) != nil {
		return
	}
	for _, item := range wrapper.Content {
		switch item.Type {
		case "text":
			if item.Text != "" {
				e := base
				e.role, e.text = RoleAssistant, item.Text
				visit(e)
			}
		case "tool_use":
			toolNames[item.ID] = item.Name
			e := base
			e.role, e.tool, e.text = RoleTool, item.Name, compactJSON(item.Input)
			visit(e)
		}
	}
}
