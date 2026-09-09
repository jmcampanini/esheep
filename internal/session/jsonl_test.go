package session

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTranscriptMovedBetweenDiscoveryAndMetadata(t *testing.T) {
	base := t.TempDir()
	active := filepath.Join(base, "sessions")
	archive := filepath.Join(base, "archived_sessions")
	path := filepath.Join(active, "moving.jsonl")
	writeTranscript(t, path, time.Now(), `{"type":"session_meta","payload":{"id":"moving"}}`)
	if err := os.Mkdir(archive, 0o755); err != nil {
		t.Fatal(err)
	}
	reader := codexAdapter{}
	transcripts, diagnostics := reader.discover(active, false)
	if len(transcripts) != 1 || len(diagnostics) != 0 {
		t.Fatalf("discover = %+v, diagnostics = %+v", transcripts, diagnostics)
	}
	if err := os.Rename(path, filepath.Join(archive, "moving.jsonl")); err != nil {
		t.Fatal(err)
	}

	meta := reader.meta(transcripts[0], nil)

	if !errors.Is(meta.err, os.ErrNotExist) {
		t.Errorf("meta after move = %v, want not-exist", meta.err)
	}
}

func TestTranscriptMoveDuringReadIsReported(t *testing.T) {
	for _, earlyStop := range []bool{false, true} {
		for _, replace := range []bool{false, true} {
			t.Run(strings.Join([]string{boolName(earlyStop, "metadata", "search"), boolName(replace, "replaced", "moved")}, "/"), func(t *testing.T) {
				base := t.TempDir()
				path := filepath.Join(base, "active.jsonl")
				archive := filepath.Join(base, "archived.jsonl")
				writeTranscript(t, path, time.Now(), `{"type":"session_meta"}`, `{"type":"event_msg"}`)

				err := forEachLine(path, func(line int, _ []byte) bool {
					if line == 1 {
						if err := os.Rename(path, archive); err != nil {
							t.Fatal(err)
						}
						if replace {
							if err := os.WriteFile(path, []byte("replacement\n"), 0o600); err != nil {
								t.Fatal(err)
							}
						}
					}
					return !earlyStop
				})

				if err == nil || !strings.Contains(err.Error(), "rerun") {
					t.Errorf("read during move = %v, want actionable failure", err)
				}
			})
		}
	}
}

func boolName(value bool, yes, no string) string {
	if value {
		return yes
	}
	return no
}
