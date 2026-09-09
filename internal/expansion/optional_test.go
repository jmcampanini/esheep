package expansion

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestExpandAbsentOptionalIncludePreservesSurroundingBytes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeInclude(t, root, "esheep-extras.md", "shared fallback must not be read")
	writeInclude(t, root, "esheep-extras-claude.md", "another harness must not be read")
	const include = "{{esheep.include-by-harness-optional \"extras\"}}"
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "lf", body: "before\n" + include + "\nafter\n", want: "before\n\nafter\n"},
		{name: "crlf", body: "before\r\n" + include + "\r\nafter\r\n", want: "before\r\n\r\nafter\r\n"},
		{name: "eof", body: "before\n" + include, want: "before\n"},
		{name: "entire body", body: include, want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := Expand([]byte(test.body), Variables{Harness: "pi", Root: root})
			if err != nil || string(got) != test.want {
				t.Errorf("Expand(%q) = %q, %v, want %q", test.body, got, err, test.want)
			}
		})
	}
}

func TestExpandPresentOptionalIncludes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{name: "empty", files: map[string]string{"esheep-extras-pi.md": ""}, want: "before\r\n\r\nafter"},
		{
			name: "recursive required include and sources",
			files: map[string]string{
				"esheep-extras-pi.md": "extras\n{{esheep.include-by-harness \"common\"}}",
				"esheep-common-pi.md": "{{esheep.sources}}",
			},
			want: "before\r\nextras\n- /alpha\r\nafter",
		},
		{
			name: "recursive optional include",
			files: map[string]string{
				"esheep-extras-pi.md": "{{esheep.include-by-harness-optional \"common\"}}",
				"esheep-common-pi.md": "nested",
			},
			want: "before\r\nnested\r\nafter",
		},
		{
			name: "missing nested optional at depth limit",
			files: map[string]string{
				"esheep-extras-pi.md": "{{esheep.include-by-harness \"common\"}}",
				"esheep-common-pi.md": "common\n{{esheep.include-by-harness-optional \"missing\"}}\nend",
			},
			want: "before\r\ncommon\n\nend\r\nafter",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			for name, body := range test.files {
				writeInclude(t, root, name, body)
			}

			got, err := Expand([]byte("before\r\n{{esheep.include-by-harness-optional \"extras\"}}\r\nafter"), Variables{Harness: "pi", Root: root, Sources: []string{"/alpha"}})
			if err != nil || string(got) != test.want {
				t.Errorf("Expand() = %q, %v, want %q", got, err, test.want)
			}
		})
	}
}

func TestExpandOptionalIncludeDoesNotSwallowContentErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		files   map[string]string
		wantErr error
		detail  string
		invalid bool
	}{
		{
			name:    "missing required nested include",
			files:   map[string]string{"esheep-extras-pi.md": "{{esheep.include-by-harness \"missing\"}}"},
			wantErr: os.ErrNotExist,
			detail:  "esheep-missing-pi.md",
		},
		{
			name:    "malformed content",
			files:   map[string]string{"esheep-extras-pi.md": "{{esheep.unknown}}"},
			invalid: true,
		},
		{
			name:   "cycle",
			files:  map[string]string{"esheep-extras-pi.md": "{{esheep.include-by-harness-optional \"extras\"}}"},
			detail: "include cycle",
		},
		{
			name: "third included file exceeds depth two",
			files: map[string]string{
				"esheep-extras-pi.md": "{{esheep.include-by-harness-optional \"common\"}}",
				"esheep-common-pi.md": "{{esheep.include-by-harness-optional \"deep\"}}",
				"esheep-deep-pi.md":   "too deep",
			},
			detail: "include depth exceeds 2",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			for name, body := range test.files {
				writeInclude(t, root, name, body)
			}

			got, err := Expand([]byte("before\n{{esheep.include-by-harness-optional \"extras\"}}\nafter"), Variables{Harness: "pi", Root: root})
			if err == nil || got != nil || !strings.Contains(err.Error(), "esheep-extras-pi.md") {
				t.Fatalf("Expand() = %q, %v, want no content and error naming the optional include", got, err)
			}
			if test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Errorf("Expand() error = %v, want %v", err, test.wantErr)
			}
			if test.detail != "" && !strings.Contains(err.Error(), test.detail) {
				t.Errorf("Expand() error = %v, want %q", err, test.detail)
			}
			var invalid *ValidationError
			if test.invalid && (!errors.As(err, &invalid) || len(invalid.Details) == 0) {
				t.Errorf("Expand() error = %v, want ValidationError with details", err)
			}
		})
	}
}

func TestExpandOptionalIncludeRejectsUnavailableEntries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		setup   func(t *testing.T, root string) string
		wantErr error
	}{
		{
			name: "broken symlink",
			setup: func(t *testing.T, root string) string {
				t.Helper()
				if err := os.Symlink("absent.md", filepath.Join(root, "esheep-extras-pi.md")); err != nil {
					t.Fatal(err)
				}
				return root
			},
			wantErr: os.ErrNotExist,
		},
		{
			name: "unavailable root",
			setup: func(_ *testing.T, root string) string {
				return filepath.Join(root, "absent")
			},
			wantErr: os.ErrNotExist,
		},
		{
			name: "broken root symlink",
			setup: func(t *testing.T, root string) string {
				t.Helper()
				link := filepath.Join(root, "root-link")
				if err := os.Symlink("absent", link); err != nil {
					t.Fatal(err)
				}
				return link
			},
			wantErr: os.ErrNotExist,
		},
		{
			name: "directory",
			setup: func(t *testing.T, root string) string {
				t.Helper()
				if err := os.Mkdir(filepath.Join(root, "esheep-extras-pi.md"), 0o700); err != nil {
					t.Fatal(err)
				}
				return root
			},
		},
		{
			name: "symlink directory cycle",
			setup: func(t *testing.T, root string) string {
				t.Helper()
				if err := os.Symlink(".", filepath.Join(root, "esheep-extras-pi.md")); err != nil {
					t.Fatal(err)
				}
				return root
			},
		},
		{
			name: "symlink loop",
			setup: func(t *testing.T, root string) string {
				t.Helper()
				if err := os.Symlink("esheep-extras-pi.md", filepath.Join(root, "esheep-extras-pi.md")); err != nil {
					t.Fatal(err)
				}
				return root
			},
			wantErr: syscall.ELOOP,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := test.setup(t, t.TempDir())

			got, err := Expand([]byte("before\n{{esheep.include-by-harness-optional \"extras\"}}"), Variables{Harness: "pi", Root: root})
			if err == nil || got != nil {
				t.Fatalf("Expand() = %q, %v, want no content and error", got, err)
			}
			if test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Errorf("Expand() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestExpandOptionalIncludeRejectsPermissionErrors(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root can read mode-000 files and directories, so it cannot prove permission-error handling")
	}
	for _, entry := range []string{"file", "root"} {
		t.Run(entry, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			path := root
			if entry == "file" {
				writeInclude(t, root, "esheep-extras-pi.md", "unreadable")
				path = filepath.Join(root, "esheep-extras-pi.md")
			}
			if err := os.Chmod(path, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.Chmod(path, 0o700); err != nil {
					t.Error(err)
				}
			})

			got, err := Expand([]byte("{{esheep.include-by-harness-optional \"extras\"}}"), Variables{Harness: "pi", Root: root})
			if !errors.Is(err, os.ErrPermission) || got != nil {
				t.Errorf("Expand() = %q, %v, want no content and permission error", got, err)
			}
		})
	}
}

func TestExpandOptionalIncludeRejectsFIFOWithoutBlocking(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "esheep-extras-pi.md")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := Expand([]byte("{{esheep.include-by-harness-optional \"extras\"}}"), Variables{Harness: "pi", Root: root})
		result <- err
	}()

	select {
	case err := <-result:
		if err == nil {
			t.Error("Expand() accepted a FIFO")
		}
	case <-time.After(2 * time.Second):
		// Release a reader blocked in open or read before waiting for it to exit.
		file, err := os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		<-result
		t.Fatal("Expand() blocked opening or reading a FIFO")
	}
}

func TestExpandOptionalIncludeFollowsExternalSymlinkWithFixedRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	writeInclude(t, outside, ".extras.md", "{{esheep.include-by-harness-optional \"common\"}}")
	writeInclude(t, outside, "esheep-common-pi.md", "wrong root")
	writeInclude(t, root, "esheep-common-pi.md", "root content")
	if err := os.Symlink(filepath.Join(outside, ".extras.md"), filepath.Join(root, "esheep-extras-pi.md")); err != nil {
		t.Fatal(err)
	}

	got, err := Expand([]byte("{{esheep.include-by-harness-optional \"extras\"}}"), Variables{Harness: "pi", Root: root})
	if err != nil || string(got) != "root content" {
		t.Errorf("Expand() = %q, %v, want root content", got, err)
	}
}
