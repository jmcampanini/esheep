package expansion

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandIncludesSelectsHarnessAndExpandsNestedVariables(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, harness := range []string{"claude", "pi", "codex"} {
		writeInclude(t, root, "esheep-body-"+harness+".md", harness+"\n{{esheep.include-by-harness \"common\"}}")
		writeInclude(t, root, "esheep-common-"+harness+".md", "{{esheep.sources}}")
	}
	body := []byte("intro\r\n{{esheep.include-by-harness \"body\"}}\r\ntail")

	for _, harness := range []string{"claude", "pi", "codex"} {
		t.Run(harness, func(t *testing.T) {
			t.Parallel()
			variables := Variables{Harness: harness, Root: root, Sources: []string{"/alpha", "/literal/{{esheep.sources}}"}}
			got, err := Expand(body, variables)
			want := "intro\r\n" + harness + "\n- /alpha\n- /literal/{{esheep.sources}}\r\ntail"
			if err != nil || string(got) != want {
				t.Fatalf("Expand() = %q, %v, want %q", got, err, want)
			}
		})
	}
}

func TestExpandIncludesAllowsRepeatedAndEmptyFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeInclude(t, root, "esheep-body-pi.md", "{{esheep.include-by-harness \"part\"}}\n{{esheep.include-by-harness \"part\"}}")
	writeInclude(t, root, "esheep-part-pi.md", "part")
	writeInclude(t, root, "esheep-empty-pi.md", "")
	body := "{{esheep.include-by-harness \"body\"}}\n{{esheep.include-by-harness \"empty\"}}\n{{esheep.include-by-harness \"body\"}}"

	got, err := Expand([]byte(body), Variables{Harness: "pi", Root: root})
	if want := "part\npart\n\npart\npart"; err != nil || string(got) != want {
		t.Fatalf("Expand() = %q, %v, want %q", got, err, want)
	}
}

func TestExpandIncludesRejectsCyclesBeforeDepthLimit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		files map[string]string
		alias string
		chain string
	}{
		{
			name:  "self",
			files: map[string]string{"esheep-body-pi.md": "{{esheep.include-by-harness \"body\"}}"},
			chain: "esheep-body-pi.md -> esheep-body-pi.md",
		},
		{
			name: "mutual",
			files: map[string]string{
				"esheep-body-pi.md":  "{{esheep.include-by-harness \"other\"}}",
				"esheep-other-pi.md": "{{esheep.include-by-harness \"body\"}}",
			},
			chain: "esheep-body-pi.md -> esheep-other-pi.md -> esheep-body-pi.md",
		},
		{
			name:  "symlink alias",
			files: map[string]string{"esheep-body-pi.md": "{{esheep.include-by-harness \"alias\"}}"},
			alias: "esheep-alias-pi.md",
			chain: "esheep-body-pi.md -> esheep-alias-pi.md",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			for name, body := range test.files {
				writeInclude(t, root, name, body)
			}
			if test.alias != "" {
				if err := os.Symlink("esheep-body-pi.md", filepath.Join(root, test.alias)); err != nil {
					t.Fatal(err)
				}
			}

			got, err := Expand([]byte("{{esheep.include-by-harness \"body\"}}"), Variables{Harness: "pi", Root: root})
			if err == nil || !strings.Contains(err.Error(), "include cycle: "+test.chain) || strings.Contains(err.Error(), "depth exceeds") {
				t.Fatalf("Expand() = %q, %v, want cycle with chain %q", got, err, test.chain)
			}
			if got != nil {
				t.Fatalf("failed expansion returned partial content: %q", got)
			}
		})
	}
}

func TestExpandIncludesEnforcesDepthLimit(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for depth := 1; depth <= MaxIncludeDepth; depth++ {
		body := "end"
		if depth < MaxIncludeDepth {
			body = fmt.Sprintf("{{esheep.include-by-harness \"level-%d\"}}", depth+1)
		}
		writeInclude(t, root, fmt.Sprintf("esheep-level-%d-pi.md", depth), body)
	}
	variables := Variables{Harness: "pi", Root: root}
	body := []byte("{{esheep.include-by-harness \"level-1\"}}")

	got, err := Expand(body, variables)
	if err != nil || string(got) != "end" {
		t.Fatalf("Expand() at limit = %q, %v, want end", got, err)
	}

	writeInclude(t, root, fmt.Sprintf("esheep-level-%d-pi.md", MaxIncludeDepth), "{{esheep.include-by-harness \"extra\"}}")
	writeInclude(t, root, "esheep-extra-pi.md", "too far")
	got, err = Expand(body, variables)
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("include depth exceeds %d", MaxIncludeDepth)) ||
		!strings.Contains(err.Error(), "esheep-extra-pi.md") || got != nil {
		t.Fatalf("Expand() beyond limit = %q, %v, want depth failure", got, err)
	}
}

func TestExpandIncludesReportsMissingAndMalformedFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	variables := Variables{Harness: "pi", Root: root}
	body := []byte("{{esheep.include-by-harness \"body\"}}")

	_, err := Expand(body, variables)
	if !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), "esheep-body-pi.md") {
		t.Fatalf("Expand() missing file error = %v", err)
	}

	writeInclude(t, root, "esheep-body-pi.md", "{{esheep.unknown}}")
	_, err = Expand(body, variables)
	var invalid *ValidationError
	if !errors.As(err, &invalid) || !strings.Contains(err.Error(), "esheep-body-pi.md") || !strings.Contains(err.Error(), "unknown esheep variable") {
		t.Fatalf("Expand() malformed file error = %v", err)
	}
}

func TestExpandIncludesFollowsExternalSymlinkWithFixedRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	writeInclude(t, outside, ".body.md", "{{esheep.include-by-harness \"common\"}}")
	writeInclude(t, outside, "esheep-common-pi.md", "wrong root")
	if err := os.Symlink(filepath.Join(outside, ".body.md"), filepath.Join(root, "esheep-body-pi.md")); err != nil {
		t.Fatal(err)
	}
	writeInclude(t, root, "esheep-common-pi.md", "root content")

	got, err := Expand([]byte("{{esheep.include-by-harness \"body\"}}"), Variables{Harness: "pi", Root: root})
	if err != nil || string(got) != "root content" {
		t.Fatalf("Expand() = %q, %v, want root content", got, err)
	}
}

func writeInclude(t *testing.T, root, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
