package expansion

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandSharedIncludesWithoutHarnessSelection(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeInclude(t, root, "esheep-body.md", "shared\n{{esheep.include-optional \"common\"}}")
	writeInclude(t, root, "esheep-common.md", "{{esheep.sources}}")
	writeInclude(t, root, "esheep-empty.md", "")
	for _, harness := range []string{"", "claude", "pi", "codex"} {
		t.Run("harness="+harness, func(t *testing.T) {
			t.Parallel()
			variables := Variables{Root: root, Harness: harness, Sources: []string{"/alpha"}}
			body := "before\r\n{{esheep.include \"body\"}}\r\n{{esheep.include-optional \"body\"}}\n{{esheep.include \"empty\"}}\nafter"

			got, err := Expand([]byte(body), variables)
			want := "before\r\nshared\n- /alpha\r\nshared\n- /alpha\n\nafter"
			if err != nil || string(got) != want {
				t.Fatalf("Expand() = %q, %v, want %q", got, err, want)
			}
		})
	}
}

func TestExpandSharedOptionalAbsencePreservesBytesWithoutFallback(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeInclude(t, root, "esheep-extras-pi.md", "must not select a harness file")
	const include = "{{esheep.include-optional \"extras\"}}"
	for _, test := range []struct {
		name string
		body string
		want string
	}{
		{name: "lf", body: "before\n" + include + "\nafter", want: "before\n\nafter"},
		{name: "crlf", body: "before\r\n" + include + "\r\nafter", want: "before\r\n\r\nafter"},
		{name: "eof", body: "before\n" + include, want: "before\n"},
		{name: "empty", body: include},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := Expand([]byte(test.body), Variables{Root: root, Harness: "pi"})
			if err != nil || string(got) != test.want {
				t.Fatalf("Expand() = %q, %v, want %q", got, err, test.want)
			}
		})
	}
}

func TestExpandMixedIncludesShareRootAndHarnessThroughSymlinks(t *testing.T) {
	t.Parallel()
	for _, outer := range []string{"include", "include-optional", "include-by-harness", "include-by-harness-optional"} {
		for _, inner := range []string{"include", "include-optional", "include-by-harness", "include-by-harness-optional"} {
			t.Run(outer+"/"+inner, func(t *testing.T) {
				t.Parallel()
				root := t.TempDir()
				outside := t.TempDir()
				fragment := filepath.Join(outside, ".fragment.md")
				writeInclude(t, outside, ".fragment.md", fmt.Sprintf("{{esheep.%s \"nested\"}}", inner))
				writeInclude(t, outside, "esheep-nested.md", "wrong root")
				writeInclude(t, outside, "esheep-nested-pi.md", "wrong root")
				for _, name := range []string{"esheep-body.md", "esheep-body-pi.md"} {
					if err := os.Symlink(fragment, filepath.Join(root, name)); err != nil {
						t.Fatal(err)
					}
				}
				writeInclude(t, root, "esheep-nested.md", "shared")
				writeInclude(t, root, "esheep-nested-pi.md", "Pi")

				got, err := Expand(fmt.Appendf(nil, "{{esheep.%s \"body\"}}", outer), Variables{Root: root, Harness: "pi"})
				want := "shared"
				if strings.Contains(inner, "by-harness") {
					want = "Pi"
				}
				if err != nil || string(got) != want {
					t.Fatalf("Expand() = %q, %v, want %q", got, err, want)
				}
			})
		}
	}
}

func TestExpandSharedIncludesRejectMissingAndInvalidFiles(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		files   map[string]string
		link    string
		wantErr error
		detail  string
	}{
		{name: "missing", wantErr: os.ErrNotExist},
		{name: "broken symlink", link: "absent.md", wantErr: os.ErrNotExist},
		{name: "directory", link: ".", detail: "not a regular file"},
		{name: "malformed variable", files: map[string]string{"esheep-body.md": "{{esheep.unknown}}"}, detail: "unknown esheep variable"},
		{
			name: "mixed cycle",
			files: map[string]string{
				"esheep-body.md":    "{{esheep.include-by-harness \"body\"}}",
				"esheep-body-pi.md": "{{esheep.include-optional \"body\"}}",
			},
			detail: "include cycle: esheep-body.md -> esheep-body-pi.md -> esheep-body.md",
		},
		{
			name:   "mixed symlink cycle",
			files:  map[string]string{"esheep-body-pi.md": "{{esheep.include-by-harness-optional \"body\"}}"},
			link:   "esheep-body-pi.md",
			detail: "include cycle: esheep-body.md -> esheep-body-pi.md",
		},
		{
			name: "mixed depth limit",
			files: map[string]string{
				"esheep-body.md":    "{{esheep.include-by-harness-optional \"body\"}}",
				"esheep-body-pi.md": "{{esheep.include \"deep\"}}",
				"esheep-deep.md":    "too deep",
			},
			detail: "include depth exceeds 2",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			for name, body := range test.files {
				writeInclude(t, root, name, body)
			}
			if test.link != "" {
				if err := os.Symlink(test.link, filepath.Join(root, "esheep-body.md")); err != nil {
					t.Fatal(err)
				}
			}
			for _, kind := range []string{"include", "include-optional"} {
				if test.name == "missing" && kind == "include-optional" {
					continue
				}

				got, err := Expand(fmt.Appendf(nil, "before\n{{esheep.%s \"body\"}}", kind), Variables{Root: root, Harness: "pi"})
				if err == nil || got != nil || (test.wantErr != nil && !errors.Is(err, test.wantErr)) {
					t.Fatalf("Expand(%s) = %q, %v, want no content and error %v", kind, got, err, test.wantErr)
				}
				if !strings.Contains(err.Error(), "esheep-body.md") || !strings.Contains(err.Error(), test.detail) {
					t.Errorf("Expand(%s) error = %v, want filename and %q", kind, err, test.detail)
				}
			}
		})
	}
}
