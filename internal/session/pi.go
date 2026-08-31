package session

import (
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strings"
)

// piAdapter reads Pi transcripts: one JSONL file per session under a
// per-project directory, with a versioned header line. A subagent transcript
// is marked by a sibling ".meta" sidecar; scratch output lives in "artifacts"
// directories that hold no transcripts.
type piAdapter struct{}

func (piAdapter) discover(root string, _ bool) ([]transcript, []Diagnostic) {
	return walkTranscripts(root, walkRules{
		classify: func(path string) (bool, bool) {
			if filepath.Ext(path) != ".jsonl" {
				return false, false
			}
			return true, fileExists(path + ".meta")
		},
		skipDir: func(entry fs.DirEntry) bool {
			return entry.Name() == "artifacts"
		},
	})
}

// piMetaLimit bounds the title prescan; session_info records land early.
const piMetaLimit = 512

func (piAdapter) meta(t transcript) (Session, error) {
	entry := Session{Harness: HarnessPi, ModifiedAt: t.modTime, Path: t.path, Subagent: t.subagent}
	err := forEachLine(t.path, func(line int, data []byte) bool {
		var record struct {
			Cwd       string `json:"cwd"`
			ID        string `json:"id"`
			Name      string `json:"name"`
			Timestamp string `json:"timestamp"`
			Type      string `json:"type"`
		}
		if json.Unmarshal(data, &record) != nil {
			return line < piMetaLimit
		}
		switch record.Type {
		case "session":
			entry.ID = record.ID
			entry.Project = record.Cwd
			entry.StartedAt = parseTimestamp(record.Timestamp)
		case "session_info":
			if entry.Title == "" {
				entry.Title = record.Name
			}
		}
		return line < piMetaLimit && (entry.ID == "" || entry.Project == "" || entry.StartedAt.IsZero() || entry.Title == "")
	})
	if err != nil {
		return entry, err
	}
	if entry.ID == "" {
		entry.ID = filepath.Base(t.path)
	}
	return entry, nil
}

func (piAdapter) scan(path string, visit func(event)) (int, error) {
	malformed := 0
	err := forEachLine(path, func(line int, data []byte) bool {
		var envelope struct {
			Message   json.RawMessage `json:"message"`
			Timestamp string          `json:"timestamp"`
			Type      string          `json:"type"`
		}
		if json.Unmarshal(data, &envelope) != nil {
			malformed++
			return true
		}
		if envelope.Type != "message" {
			return true
		}
		piMessageEvents(envelope.Message, event{line: line, timestamp: parseTimestamp(envelope.Timestamp)}, visit)
		return true
	})
	return malformed, err
}

func piMessageEvents(raw json.RawMessage, base event, visit func(event)) {
	var message struct {
		Content []struct {
			Arguments json.RawMessage `json:"arguments"`
			Name      string          `json:"name"`
			Text      string          `json:"text"`
			Type      string          `json:"type"`
		} `json:"content"`
		IsError  *bool  `json:"isError"`
		Role     string `json:"role"`
		ToolName string `json:"toolName"`
	}
	if json.Unmarshal(raw, &message) != nil {
		return
	}
	switch message.Role {
	case "user", "assistant":
		role := RoleUser
		if message.Role == "assistant" {
			role = RoleAssistant
		}
		for _, block := range message.Content {
			switch block.Type {
			case "text":
				if block.Text != "" {
					e := base
					e.role, e.text = role, block.Text
					visit(e)
				}
			case "toolCall":
				e := base
				e.role, e.tool, e.text = RoleTool, block.Name, compactJSON(block.Arguments)
				visit(e)
			}
		}
	case "toolResult":
		parts := make([]string, 0, len(message.Content))
		for _, block := range message.Content {
			if block.Text != "" {
				parts = append(parts, block.Text)
			}
		}
		base.role, base.tool, base.text = RoleTool, message.ToolName, strings.Join(parts, "\n")
		base.failed = message.IsError != nil && *message.IsError
		visit(base)
	}
}
