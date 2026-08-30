package skill

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestParsePreservesCRLFBodyAndOptionalPresence(t *testing.T) {
	t.Parallel()
	body := []byte("# Hello\r\n\x00tail")
	input := append([]byte("---\r\nname: demo-skill\r\ndescription: ' useful '\r\nlicense: ''\r\ncompatibility: ''\r\nmetadata: {}\r\nclaude:\r\n  argument-hint: ''\r\npi:\r\n  disabled: true\r\ncodex:\r\n  disabled: true\r\nagents:\r\n  disabled: false\r\n---\r\n"), body...)
	document, err := Parse(input, "demo-skill")
	if err != nil {
		t.Fatal(err)
	}
	if string(document.Body) != string(body) {
		t.Fatalf("body = %q, want %q", document.Body, body)
	}
	if document.License == nil || *document.License != "" || document.Compatibility == nil || *document.Compatibility != "" {
		t.Fatalf("optional strings = license %#v, compatibility %#v", document.License, document.Compatibility)
	}
	if document.Metadata == nil || len(document.Metadata) != 0 {
		t.Fatalf("metadata = %#v, want present empty map", document.Metadata)
	}
	if document.Targets.Claude.ArgumentHint == nil || *document.Targets.Claude.ArgumentHint != "" {
		t.Fatalf("argument hint = %#v, want present empty string", document.Targets.Claude.ArgumentHint)
	}
	if !document.Targets.Pi.Disabled || !document.Targets.Codex.Disabled {
		t.Fatalf("targets = %#v", document.Targets)
	}
}

func TestParseDistinguishesAbsentOptionalFields(t *testing.T) {
	t.Parallel()
	document, err := Parse([]byte("---\nname: demo\ndescription: ok\n---\n"), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if document.License != nil || document.Compatibility != nil || document.Metadata != nil || document.Targets.Claude.ArgumentHint != nil {
		t.Fatalf("absent fields became present: %#v", document)
	}
}

func TestParsePreservesUninterpretedFieldsInOrder(t *testing.T) {
	t.Parallel()
	input := []byte("---\n" +
		"name: demo\n" +
		"allowed-tools: Bash\n" +
		"description: ok\n" +
		"disable-model-invocation: true\n" +
		"hooks:\n" +
		"  PreToolUse:\n" +
		"    - matcher: Bash\n" +
		"claude:\n" +
		"  disabled: true\n" +
		"x-owner: engineering\n" +
		"---\nbody")
	document, err := Parse(input, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if !document.DisableModelInvocation {
		t.Fatal("DisableModelInvocation = false, want true")
	}
	if !document.Targets.Claude.Disabled {
		t.Fatal("claude target not disabled")
	}
	keys := make([]string, 0, len(document.Extra))
	for _, field := range document.Extra {
		keys = append(keys, field.Key)
	}
	want := []string{"allowed-tools", "hooks", "x-owner"}
	if len(keys) != len(want) {
		t.Fatalf("Extra keys = %v, want %v", keys, want)
	}
	for index := range want {
		if keys[index] != want[index] {
			t.Fatalf("Extra keys = %v, want %v", keys, want)
		}
	}
	for _, field := range document.Extra {
		if field.Value == nil {
			t.Fatalf("Extra field %q has nil value", field.Key)
		}
	}
}

func TestParseRejectsInvalidDeclarativeFormat(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		dir  string
		yaml string
		code Code
	}{
		{name: "missing frontmatter", dir: "demo", yaml: "name: demo\n", code: CodeFrontmatter},
		{name: "duplicate common key", dir: "demo", yaml: "name: demo\nname: demo\ndescription: ok\n", code: CodeYAML},
		{name: "duplicate target key", dir: "demo", yaml: "name: demo\ndescription: ok\nclaude:\n  disabled: true\n  disabled: false\n", code: CodeYAML},
		{name: "duplicate uninterpreted key", dir: "demo", yaml: "name: demo\ndescription: ok\nextra: 1\nextra: 2\n", code: CodeInvalidValue},
		{name: "unknown target field", dir: "demo", yaml: "name: demo\ndescription: ok\nclaude:\n  hooks: run\n", code: CodeUnknownField},
		{name: "codex hint", dir: "demo", yaml: "name: demo\ndescription: ok\ncodex:\n  argument-hint: no\n", code: CodeUnknownField},
		{name: "invocation toggle is string", dir: "demo", yaml: "name: demo\ndescription: ok\ndisable-model-invocation: 'yes'\n", code: CodeInvalidValue},
		{name: "name is map", dir: "demo", yaml: "name: {}\ndescription: ok\n", code: CodeInvalidValue},
		{name: "license is boolean", dir: "demo", yaml: "name: demo\ndescription: ok\nlicense: true\n", code: CodeInvalidValue},
		{name: "metadata is scalar", dir: "demo", yaml: "name: demo\ndescription: ok\nmetadata: no\n", code: CodeInvalidValue},
		{name: "metadata value type", dir: "demo", yaml: "name: demo\ndescription: ok\nmetadata:\n  count: 2\n", code: CodeInvalidValue},
		{name: "target is scalar", dir: "demo", yaml: "name: demo\ndescription: ok\nclaude: no\n", code: CodeInvalidValue},
		{name: "disabled is string", dir: "demo", yaml: "name: demo\ndescription: ok\nclaude:\n  disabled: 'true'\n", code: CodeInvalidValue},
		{name: "hint is boolean", dir: "demo", yaml: "name: demo\ndescription: ok\npi:\n  argument-hint: true\n", code: CodeInvalidValue},
		{name: "invalid name", dir: "bad_name", yaml: "name: bad_name\ndescription: ok\n", code: CodeInvalidName},
		{name: "directory mismatch", dir: "other", yaml: "name: demo\ndescription: ok\n", code: CodeNameMismatch},
		{name: "blank description", dir: "demo", yaml: "name: demo\ndescription: '  '\n", code: CodeRequiredField},
		{name: "long description", dir: "demo", yaml: "name: demo\ndescription: " + strings.Repeat("界", 1025) + "\n", code: CodeInvalidValue},
		{name: "long compatibility", dir: "demo", yaml: "name: demo\ndescription: ok\ncompatibility: " + strings.Repeat("界", 501) + "\n", code: CodeInvalidValue},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := []byte(test.yaml)
			if test.code != CodeFrontmatter {
				input = []byte("---\n" + test.yaml + "---\nbody")
			}
			_, err := Parse(input, test.dir)
			if err == nil {
				t.Fatal("Parse succeeded")
			}
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("error = %T, want ValidationError", err)
			}
			for _, diagnostic := range validationErr.Diagnostics {
				if diagnostic.Code == test.code {
					return
				}
			}
			t.Fatalf("diagnostics = %#v, want code %q", validationErr.Diagnostics, test.code)
		})
	}
}

func TestLoadRejectsManifestSymlinkWithoutReadingThroughIt(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "demo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "manifest-target")
	if err := os.WriteFile(target, []byte("---\nname: demo\ndescription: ok\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("manifest-target", filepath.Join(root, "SKILL.md")); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(root)
	if err == nil {
		t.Fatal("Load accepted a manifest symlink")
	}
	if loaded.Root != root {
		t.Fatalf("package root = %q, want %q", loaded.Root, root)
	}
	diagnostics := ErrorDiagnostics(err)
	if len(diagnostics) != 1 || diagnostics[0].Code != CodeInvalidSymlink || diagnostics[0].Path != "SKILL.md" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestLoadRejectsManifestFIFOWithoutBlocking(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "demo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(root, "SKILL.md"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := Load(root)
		result <- err
	}()
	select {
	case err := <-result:
		diagnostics := ErrorDiagnostics(err)
		if len(diagnostics) != 1 || diagnostics[0].Code != CodeUnsupportedFile || diagnostics[0].Path != "SKILL.md" {
			t.Fatalf("diagnostics = %#v", diagnostics)
		}
	case <-time.After(time.Second):
		t.Fatal("Load blocked opening a manifest FIFO")
	}
}

func TestLoadRejectsManifestUnixSocket(t *testing.T) {
	t.Parallel()
	parent, err := os.MkdirTemp("", "esheep-socket-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(parent) })
	root := filepath.Join(parent, "demo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", filepath.Join(root, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	_, err = Load(root)
	diagnostics := ErrorDiagnostics(err)
	if len(diagnostics) != 1 || diagnostics[0].Code != CodeUnsupportedFile || diagnostics[0].Path != "SKILL.md" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestValidateReadableRegularRejectsFIFOWithoutBlocking(t *testing.T) {
	t.Parallel()
	rootPath := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(rootPath, "support"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	result := make(chan error, 1)
	go func() {
		result <- validateReadableRegular(root, "support")
	}()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("validateReadableRegular accepted a FIFO")
		}
	case <-time.After(time.Second):
		t.Fatal("validateReadableRegular blocked opening a FIFO")
	}
}

func TestValidateTreeRejectsPortablePathCollisionsAndTopology(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		directories []string
		files       []File
	}{
		{name: "case collision", files: []File{{Path: "Docs/Guide"}, {Path: "docs/guide"}}},
		{name: "NFC collision", files: []File{{Path: "café"}, {Path: "cafe\u0301"}}},
		{name: "generated manifest collision", files: []File{{Path: "skill.md"}}},
		{name: "file contains directory", directories: []string{"parent/child"}, files: []File{{Path: "PARENT"}}},
		{name: "path descends through file", files: []File{{Path: "parent"}, {Path: "PARENT/child"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			diagnostics := ValidateTree(Package{Directories: test.directories, Files: test.files})
			if len(diagnostics) != 1 || diagnostics[0].Code != CodePathCollision {
				t.Fatalf("diagnostics = %#v", diagnostics)
			}
		})
	}
}

func TestParseCountsUnicodeCharactersAtLimits(t *testing.T) {
	t.Parallel()
	input := "---\nname: demo\ndescription: " + strings.Repeat("界", 1024) + "\ncompatibility: " + strings.Repeat("界", 500) + "\n---\n"
	if _, err := Parse([]byte(input), "demo"); err != nil {
		t.Fatal(err)
	}
}
