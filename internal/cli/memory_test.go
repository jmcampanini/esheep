package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jmcampanini/esheep/internal/config"
	"github.com/jmcampanini/esheep/internal/memory"
)

func TestMemoryRecordPassesRequestAndPrintsPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "memories")
	load := func(config.LoadOptions) (config.LoadResult, error) {
		return config.LoadResult{ResolvedMemory: root}, nil
	}
	var captured memory.Request
	operations := commandOperations{memoryRecord: func(_ context.Context, request memory.Request) (memory.Result, error) {
		captured = request
		return memory.Result{Diagnostics: []string{"note about git"}, Path: filepath.Join(root, "p", "memory", "s.jsonl")}, nil
	}}
	before := time.Now()

	code, stdout, stderr := runCommandWithOperations(t, load, operations,
		"memory", "record", "remember this", "--why", "because", "--source", "a.md", "--source", "https://x.test", "--session", "manual-1")

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if stdout != filepath.Join(root, "p", "memory", "s.jsonl")+"\n" {
		t.Fatalf("stdout = %q", stdout)
	}
	if stderr != "note about git\n" {
		t.Fatalf("stderr = %q", stderr)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if captured.Time.Before(before) || captured.Time.After(time.Now()) {
		t.Fatalf("time = %v, want between %v and now", captured.Time, before)
	}
	if captured.Env["PATH"] != os.Getenv("PATH") {
		t.Fatalf("env PATH = %q, want the process environment", captured.Env["PATH"])
	}
	captured.Env, captured.Time = nil, time.Time{}
	want := memory.Request{Cwd: cwd, Root: root, Session: "manual-1", Sources: []string{"a.md", "https://x.test"}, Text: "remember this", Why: "because"}
	if !reflect.DeepEqual(captured, want) {
		t.Fatalf("request = %#v, want %#v", captured, want)
	}
}

func TestMemoryRecordReadsStdinForDash(t *testing.T) {
	load := func(config.LoadOptions) (config.LoadResult, error) {
		return config.LoadResult{ResolvedMemory: t.TempDir()}, nil
	}
	var captured memory.Request
	operations := commandOperations{memoryRecord: func(_ context.Context, request memory.Request) (memory.Result, error) {
		captured = request
		return memory.Result{Path: "/p"}, nil
	}}

	code, _, stderr := runCommandWithInput(t, load, operations, "line one\nline two\n", "memory", "record", "-")

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if captured.Text != "line one\nline two\n" {
		t.Fatalf("text = %q", captured.Text)
	}
}

func TestMemoryRecordInvalidInputFailsBeforeConfiguration(t *testing.T) {
	tests := []struct {
		args  []string
		name  string
		code  int
		stdin string
		want  string
	}{
		{code: 1, name: "blank operand", args: []string{"memory", "record", "  "}, want: "TEXT must not be blank"},
		{code: 1, name: "blank stdin", args: []string{"memory", "record", "-"}, stdin: "\n", want: "TEXT must not be blank"},
		{code: 2, name: "missing operand", args: []string{"memory", "record"}, want: "accepts 1 arg"},
		{code: 1, name: "unsafe session", args: []string{"memory", "record", "t", "--session", "a/b"}, want: "--session"},
		{code: 1, name: "blank source", args: []string{"memory", "record", "t", "--source", " "}, want: "--source must not be blank"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			loads := 0
			load := func(config.LoadOptions) (config.LoadResult, error) {
				loads++
				return config.LoadResult{}, nil
			}
			operations := commandOperations{memoryRecord: func(context.Context, memory.Request) (memory.Result, error) {
				t.Fatal("record was called")
				return memory.Result{}, nil
			}}

			code, stdout, stderr := runCommandWithInput(t, load, operations, test.stdin, test.args...)

			if code != test.code || stdout != "" || !strings.Contains(stderr, test.want) || loads != 0 {
				t.Fatalf("exit = %d, stdout = %q, stderr = %q, loads = %d", code, stdout, stderr, loads)
			}
		})
	}
}

func TestMemoryRecordFailureIsAnApplicationError(t *testing.T) {
	load := func(config.LoadOptions) (config.LoadResult, error) {
		return config.LoadResult{ResolvedMemory: t.TempDir()}, nil
	}
	operations := commandOperations{memoryRecord: func(context.Context, memory.Request) (memory.Result, error) {
		return memory.Result{}, errors.New("no session detected")
	}}

	code, stdout, stderr := runCommandWithOperations(t, load, operations, "memory", "record", "text")

	if code != 1 || stdout != "" || stderr != "esheep: no session detected\n" {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
}

func TestMemoryRecordReportsOutputFailureAfterAppend(t *testing.T) {
	for _, diagnostic := range []bool{false, true} {
		t.Run(fmt.Sprint("diagnostic=", diagnostic), func(t *testing.T) {
			calls := 0
			operations := commandOperations{memoryRecord: func(context.Context, memory.Request) (memory.Result, error) {
				calls++
				result := memory.Result{Path: "/memories/project/memory/session.jsonl"}
				if diagnostic {
					result.Diagnostics = []string{"git is unavailable"}
				}
				return result, nil
			}}
			root := newRootCommandWithOperations(successfulLoad, operations)
			var output bytes.Buffer
			root.SetOut(&output)
			root.SetErr(&output)
			if diagnostic {
				root.SetErr(failingWriter{})
			} else {
				root.SetOut(failingWriter{})
			}
			if code := execute(root, []string{"memory", "record", "text", "--session", "manual"}); code != 1 || calls != 1 {
				t.Fatalf("exit = %d, appends = %d, want one append and output failure", code, calls)
			}
		})
	}
}
