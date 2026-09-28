// Package memory appends session memories to esheep's memory root.
//
// A memory is one JSON line appended to a file named by the calling
// harness session inside a directory named by the project the session works
// in. The package never reads, rewrites, or deletes existing memories.
package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// Session environment variables in precedence order. A harness spawned by
// another inherits the outer variable, and Codex is nested inside Pi and
// Claude far more often than it nests them, so the innermost harness wins.
var sessionVariables = []string{"CODEX_THREAD_ID", "PI_SESSION_ID", "CLAUDE_CODE_SESSION_ID"}

// Request describes one memory to append.
type Request struct {
	// Cwd is the working directory the project identity derives from.
	Cwd string
	// Env is the process environment consulted for the session ID.
	Env map[string]string
	// Root is the memory root directory.
	Root string
	// Session overrides environment detection when non-empty. Callers
	// validate it with ValidateSession before submitting.
	Session string
	// Sources are optional pointers backing the memory.
	Sources []string
	// Text is the memory. Callers reject blank text before submitting.
	Text string
	// Time is the instant recorded on the entry.
	Time time.Time
	// Why is an optional reason the memory holds.
	Why string
}

// Result reports where a memory was appended.
type Result struct {
	// Diagnostics are non-fatal notes about how the project was identified.
	Diagnostics []string
	// Path is the file the entry was appended to.
	Path string
}

// entry is the JSON line written for one memory; field order is the key order.
type entry struct {
	Time    string   `json:"time"`
	Text    string   `json:"text"`
	Why     string   `json:"why,omitempty"`
	Sources []string `json:"sources,omitempty"`
}

// Record appends one memory to <root>/<project>/memory/<session>.jsonl,
// creating directories and the file as needed. The append is a single
// write ending in a newline so concurrent writers to one file never
// interleave inside an entry.
func Record(ctx context.Context, request Request) (Result, error) {
	session, err := resolveSession(request.Session, request.Env)
	if err != nil {
		return Result{}, err
	}
	project, diagnostics, err := resolveProject(ctx, request.Cwd)
	if err != nil {
		return Result{}, err
	}

	line, err := json.Marshal(entry{
		Time:    request.Time.Format(time.RFC3339),
		Text:    request.Text,
		Why:     request.Why,
		Sources: request.Sources,
	})
	if err != nil {
		return Result{}, fmt.Errorf("encode memory: %w", err)
	}
	line = append(line, '\n')

	directory := filepath.Join(request.Root, Segment(project), "memory")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return Result{}, fmt.Errorf("create memory directory: %w", err)
	}
	path := filepath.Join(directory, session+".jsonl")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return Result{}, fmt.Errorf("open memory file: %w", err)
	}
	_, writeErr := file.Write(line)
	closeErr := file.Close()
	if writeErr != nil {
		return Result{}, fmt.Errorf("append memory: %w", writeErr)
	}
	if closeErr != nil {
		return Result{}, fmt.Errorf("close memory file: %w", closeErr)
	}
	return Result{Diagnostics: diagnostics, Path: path}, nil
}

// Segment is the directory name for a project identity: every path
// separator becomes a hyphen, so github.com/acme/widgets is
// github.com-acme-widgets and /Users/me/scratch is -Users-me-scratch.
func Segment(project string) string {
	return strings.ReplaceAll(project, "/", "-")
}

// ValidateSession rejects session IDs that cannot serve as a file name:
// empty values, path separators, "..", whitespace, and control characters.
func ValidateSession(session string) error {
	if session == "" {
		return errors.New("session ID must not be empty")
	}
	if strings.ContainsAny(session, `/\`) || strings.Contains(session, "..") {
		return fmt.Errorf("session ID %q must not contain path separators or \"..\"", session)
	}
	if strings.ContainsFunc(session, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return fmt.Errorf("session ID %q must not contain whitespace or control characters", session)
	}
	return nil
}

func resolveSession(override string, env map[string]string) (string, error) {
	if override != "" {
		return override, nil
	}
	for _, name := range sessionVariables {
		value := env[name]
		if value == "" {
			continue
		}
		if err := ValidateSession(value); err != nil {
			return "", fmt.Errorf("%s: %w", name, err)
		}
		return value, nil
	}
	return "", fmt.Errorf("no session detected: set --session, or run from a harness that sets %s", strings.Join(sessionVariables, ", "))
}
