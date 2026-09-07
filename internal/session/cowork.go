package session

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// coworkAdapter reads one audit per local Cowork conversation. The two scope
// directories are opaque; companion metadata lives beside the session folder.
type coworkAdapter struct {
	root string
}

func (coworkAdapter) discover(root string, _ bool) ([]transcript, []Diagnostic) {
	return walkJSONLTranscripts(root, walkRules{
		accept: func(path string) bool {
			relative, err := filepath.Rel(root, path)
			return err == nil && len(strings.Split(relative, string(filepath.Separator))) == 4 && filepath.Base(path) == "audit.jsonl"
		},
		skipDir: func(path string, entry fs.DirEntry) bool {
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return true
			}
			depth := len(strings.Split(relative, string(filepath.Separator)))
			return depth > 3 || (depth == 3 && (!strings.HasPrefix(entry.Name(), "local_") || entry.Name() == "local_"))
		},
	})
}

func (a coworkAdapter) meta(t transcript) describedSession {
	id, err := filepath.Rel(a.root, filepath.Dir(t.path))
	meta := describedSession{err: err, session: Session{
		Harness: HarnessClaudeCowork, ID: filepath.ToSlash(id), ModifiedAt: t.modTime, Path: t.path,
	}}
	if err != nil {
		return meta
	}
	companion, diagnostics := coworkCompanion(filepath.Dir(t.path))
	meta.diagnostics = diagnostics
	meta.session.Archived = companion.IsArchived
	meta.session.Projects = projectPaths(companion.UserSelectedFolders...)
	meta.session.Title = companion.Title
	if companion.CreatedAt > 0 {
		started := time.UnixMilli(companion.CreatedAt).UTC()
		if started.Year() <= 9999 {
			meta.session.StartedAt = started
		}
	}

	decoder := coworkDecoder{toolNames: make(map[string]map[string]string)}
	meta.err = forEachLine(t.path, func(line int, data []byte) bool {
		var envelope coworkEnvelope
		if json.Unmarshal(data, &envelope) != nil {
			return true
		}
		base := envelope.event(line)
		if meta.session.StartedAt.IsZero() {
			meta.session.StartedAt = base.timestamp
		}
		decoder.decode(envelope, base, func(event) { meta.eligible = true })
		return !meta.eligible || meta.session.StartedAt.IsZero()
	})
	return meta
}

type coworkMetadata struct {
	CreatedAt           int64    `json:"createdAt"`
	IsArchived          bool     `json:"isArchived"`
	SessionID           string   `json:"sessionId"`
	Title               string   `json:"title"`
	UserSelectedFolders []string `json:"userSelectedFolders"`
}

func coworkCompanion(directory string) (coworkMetadata, []Diagnostic) {
	path := directory + ".json"
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return coworkMetadata{}, nil
	}
	var data []byte
	if err == nil {
		data, err = readCoworkCompanion(path, info)
	}
	if err != nil {
		return coworkMetadata{}, []Diagnostic{{Code: codeMetadataRead, Harness: HarnessClaudeCowork, Path: path, Message: err.Error()}}
	}
	var metadata coworkMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return coworkMetadata{}, []Diagnostic{{Code: codeMetadataInvalid, Harness: HarnessClaudeCowork, Path: path, Message: "invalid companion metadata"}}
	}
	if metadata.SessionID != filepath.Base(directory) {
		return coworkMetadata{}, []Diagnostic{{Code: codeMetadataInvalid, Harness: HarnessClaudeCowork, Path: path, Message: "companion sessionId does not match its session directory"}}
	}
	return metadata, nil
}

func readCoworkCompanion(path string, info os.FileInfo) ([]byte, error) {
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("companion %q is not a regular file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	current, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("verify companion after reading; rerun the command: %w", err)
	}
	if !os.SameFile(info, current) {
		return nil, fmt.Errorf("companion %q was replaced while reading; rerun the command", path)
	}
	return data, nil
}

type coworkEnvelope struct {
	AuditTimestamp  string          `json:"_audit_timestamp"`
	IsMeta          bool            `json:"isMeta"`
	Message         json.RawMessage `json:"message"`
	ParentToolUseID string          `json:"parent_tool_use_id"`
	Timestamp       string          `json:"timestamp"`
	Type            string          `json:"type"`
}

func (e coworkEnvelope) event(line int) event {
	timestamp := parseTimestamp(e.Timestamp)
	if timestamp.IsZero() {
		timestamp = parseTimestamp(e.AuditTimestamp)
	}
	return event{line: line, subagent: e.ParentToolUseID != "", timestamp: timestamp}
}

type coworkDecoder struct {
	toolNames map[string]map[string]string
}

func (d *coworkDecoder) decode(envelope coworkEnvelope, base event, visit func(event)) {
	if envelope.Type != "user" && envelope.Type != "assistant" {
		return
	}
	names := d.toolNames[envelope.ParentToolUseID]
	if names == nil {
		names = make(map[string]string)
		d.toolNames[envelope.ParentToolUseID] = names
	}
	switch envelope.Type {
	case "user":
		claudeUserEvents(envelope.Message, envelope.IsMeta, base, names, visit)
	case "assistant":
		claudeAssistantEvents(envelope.Message, base, names, visit)
	}
}

func (coworkAdapter) scan(path string, visit func(event)) (int, error) {
	malformed := 0
	decoder := coworkDecoder{toolNames: make(map[string]map[string]string)}
	err := forEachLine(path, func(line int, data []byte) bool {
		var envelope coworkEnvelope
		if json.Unmarshal(data, &envelope) != nil {
			malformed++
			return true
		}
		decoder.decode(envelope, envelope.event(line), visit)
		return true
	})
	return malformed, err
}
