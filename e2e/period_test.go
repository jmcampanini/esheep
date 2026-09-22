package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessionPeriodWorkflow(t *testing.T) {
	home := filepath.Join(workDir, "period", "home")
	path := filepath.Join(home, ".claude", "projects", "project", "older.jsonl")
	writeSessionTranscript(t, path,
		`{"type":"user","timestamp":"2026-08-20T10:00:00Z","message":{"content":"retry August"}}`,
		`{"type":"user","timestamp":"2026-09-18T12:00:00Z","message":{"content":"retry September"}}`,
	)
	old := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	environment := map[string]string{"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, "config"), "TZ": "America/New_York"}
	before := snapshotTree(t, home)
	for _, command := range [][]string{{"list"}, {"search", "retry"}} {
		args := append([]string{"sessions"}, command...)
		args = append(args, "--harness", "claude", "--since", "2026-09-14", "--until", "2026-09-21", "--json")

		result := runEsheep(t, environment, args...)

		assertSuccess(t, result)
		var report struct {
			Complete bool `json:"complete"`
			Period   struct {
				Since string `json:"since"`
				Until string `json:"until"`
			} `json:"period"`
			Sessions []struct {
				ID        string `json:"id"`
				StartedAt string `json:"started_at"`
				Hits      []struct {
					Line      int    `json:"line"`
					Timestamp string `json:"timestamp"`
				} `json:"hits"`
			} `json:"sessions"`
		}
		if err := json.Unmarshal([]byte(result.stdout), &report); err != nil {
			t.Fatal(err)
		}
		if !report.Complete || len(report.Sessions) != 1 || report.Sessions[0].ID != "older" || report.Sessions[0].StartedAt != "2026-08-20T10:00:00Z" {
			t.Fatalf("%s report = %+v, want older session", command[0], report)
		}
		if report.Period.Since != "2026-09-14T00:00:00-04:00" || report.Period.Until != "2026-09-22T00:00:00-04:00" {
			t.Errorf("period = %+v, want resolved local calendar bounds", report.Period)
		}
		if command[0] == "search" && (len(report.Sessions[0].Hits) != 1 || report.Sessions[0].Hits[0].Line != 2 || report.Sessions[0].Hits[0].Timestamp != "2026-09-18T12:00:00Z") {
			t.Errorf("hits = %+v, want only the September event", report.Sessions[0].Hits)
		}
	}
	invalid := runEsheep(t, environment, "sessions", "search", ".", "--raw", "--since", "7d")
	if invalid.exitCode != 2 || !strings.Contains(invalid.stderr, "--raw cannot combine with --since or --until") {
		t.Errorf("raw period result = %+v, want usage error", invalid)
	}
	if got := snapshotTree(t, home); got != before {
		t.Fatal("period commands modified the session store")
	}
}
