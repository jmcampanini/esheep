package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/jmcampanini/esheep/internal/config"
	"github.com/jmcampanini/esheep/internal/session"
)

func TestColorFlagRejectsUnknownModeBeforeAnyWork(t *testing.T) {
	t.Parallel()
	loads := 0
	load := func(config.LoadOptions) (config.LoadResult, error) {
		loads++
		return config.LoadResult{}, nil
	}

	code, stdout, stderr := runCommand(t, load, "--color", "sometimes", "sessions", "list")

	if code != 2 || stdout != "" || loads != 0 {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q, loads = %d", code, stdout, stderr, loads)
	}
	if !strings.Contains(stderr, `"sometimes"`) || !strings.Contains(stderr, "--color") || strings.Count(stderr, "\n") != 1 {
		t.Fatalf("stderr = %q, want one line naming the value and the flag", stderr)
	}
}

func TestColorAlwaysStylesHumanOutputButNeverJSON(t *testing.T) {
	t.Parallel()
	load := sessionLoader(t)
	operations := commandOperations{
		sessionList: func(context.Context, session.Roots, session.Filter) session.ListReport {
			return session.ListReport{
				Complete: true,
				Sessions: []session.Session{{Harness: session.HarnessClaude, ID: "abc", Path: "/roots/claude/p/abc.jsonl"}},
			}
		},
	}

	code, human, stderr := runCommandWithOperations(t, load, operations, "sessions", "list", "--color=always")
	if code != 0 || !strings.ContainsRune(human, '\x1b') {
		t.Fatalf("human: exit = %d, stderr = %q, stdout = %q, want escapes", code, stderr, human)
	}

	code, document, stderr := runCommandWithOperations(t, load, operations, "sessions", "list", "--json", "--color=always")
	if code != 0 || strings.ContainsRune(document, '\x1b') {
		t.Fatalf("json: exit = %d, stderr = %q, stdout = %q, want no escapes", code, stderr, document)
	}

	code, plain, stderr := runCommandWithOperations(t, load, operations, "sessions", "list", "--color=never")
	if code != 0 || strings.ContainsRune(plain, '\x1b') {
		t.Fatalf("never: exit = %d, stderr = %q, stdout = %q, want no escapes", code, stderr, plain)
	}
}
