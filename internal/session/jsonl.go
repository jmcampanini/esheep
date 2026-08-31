package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// forEachLine streams a transcript file line by line, calling visit with
// 1-based line numbers until it returns false or the file ends. Lines can be
// arbitrarily long; the trailing newline is trimmed.
func forEachLine(path string, visit func(line int, data []byte) bool) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	reader := bufio.NewReaderSize(file, 64*1024)
	line := 0
	for {
		data, err := reader.ReadBytes('\n')
		if len(data) > 0 {
			line++
			if !visit(line, bytes.TrimRight(data, "\r\n")) {
				return nil
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// walkRules parameterizes transcript discovery for one harness grammar.
type walkRules struct {
	// file reports whether a regular file is a transcript and whether it
	// belongs to a subagent.
	file func(path string, entry fs.DirEntry) (include bool, subagent bool)
	// skipDir reports whether a directory subtree holds no wanted transcripts.
	skipDir func(entry fs.DirEntry) bool
}

// walkTranscripts discovers transcript files under root, tolerating and
// recording per-entry failures instead of aborting.
func walkTranscripts(root string, rules walkRules) ([]transcript, []Diagnostic) {
	var transcripts []transcript
	var diagnostics []Diagnostic
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Code: codeWalk, Message: err.Error(), Path: path})
			return nil
		}
		if entry.IsDir() {
			if path != root && rules.skipDir != nil && rules.skipDir(entry) {
				return fs.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		include, subagent := rules.file(path, entry)
		if !include {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			diagnostics = append(diagnostics, Diagnostic{Code: codeWalk, Message: infoErr.Error(), Path: path})
			return nil
		}
		transcripts = append(transcripts, transcript{modTime: info.ModTime(), path: path, subagent: subagent})
		return nil
	})
	return transcripts, diagnostics
}

// flattenText extracts human-readable text from a value that is either a
// JSON string or an array of content blocks carrying "text" fields.
func flattenText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return joinNonEmpty(parts)
}

func joinNonEmpty(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	default:
		var b bytes.Buffer
		for index, part := range parts {
			if index > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(part)
		}
		return b.String()
	}
}

// compactJSON renders raw JSON on one line for matching and excerpts.
func compactJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var b bytes.Buffer
	if json.Compact(&b, raw) != nil {
		return string(raw)
	}
	return b.String()
}

// parseTimestamp reads an RFC 3339 timestamp, returning zero on failure.
func parseTimestamp(value string) time.Time {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return t
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
