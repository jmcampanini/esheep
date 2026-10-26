package skill

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestParsePreservesCRLFBodyAndOptionalPresence(t *testing.T) {
	t.Parallel()
	body := []byte("# Hello\r\n\x00tail")
	input := append([]byte("---\r\nname: demo-skill\r\nesheep-trigger: ' useful '\r\nlicense: ''\r\ncompatibility: ''\r\nmetadata: {}\r\nesheep-targets: [claude, pi: [work]]\r\nesheep-only-layers: [work]\r\n---\r\n"), body...)
	document, err := Parse(input, "demo-skill", "SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(document.Body) != string(body) {
		t.Fatalf("body = %q, want %q", document.Body, body)
	}
	if document.Trigger != " useful " {
		t.Fatalf("trigger = %q, want preserved whitespace", document.Trigger)
	}
	if document.License == nil || *document.License != "" || document.Compatibility == nil || *document.Compatibility != "" {
		t.Fatalf("optional strings = license %#v, compatibility %#v", document.License, document.Compatibility)
	}
	if document.Metadata == nil || len(document.Metadata) != 0 {
		t.Fatalf("metadata = %#v, want present empty map", document.Metadata)
	}
	if !document.Targets.Claude.Listed || !document.Targets.Pi.Listed || document.Targets.Codex.Listed {
		t.Fatalf("targets = %#v", document.Targets)
	}
	if document.Targets.Claude.OnlyLayers != nil {
		t.Fatalf("claude gate = %#v, want none", document.Targets.Claude.OnlyLayers)
	}
	if len(document.Targets.Pi.OnlyLayers) != 1 || document.Targets.Pi.OnlyLayers[0] != "work" {
		t.Fatalf("pi gate = %#v, want [work]", document.Targets.Pi.OnlyLayers)
	}
	if len(document.OnlyLayers) != 1 || document.OnlyLayers[0] != "work" {
		t.Fatalf("only layers = %#v, want [work]", document.OnlyLayers)
	}
}

func TestParseDistinguishesAbsentOptionalFields(t *testing.T) {
	t.Parallel()
	document, err := Parse([]byte("---\nname: demo\nesheep-trigger: ok\nesheep-targets: [claude]\n---\n"), "demo", "SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if document.License != nil || document.Compatibility != nil || document.Metadata != nil || document.OnlyLayers != nil {
		t.Fatalf("absent fields became present: %#v", document)
	}
}

func TestParsePreservesUninterpretedFieldsInOrder(t *testing.T) {
	t.Parallel()
	input := []byte("---\n" +
		"name: demo\n" +
		"allowed-tools: Bash\n" +
		"esheep-trigger: ok\n" +
		"disable-model-invocation: true\n" +
		"hooks:\n" +
		"  PreToolUse:\n" +
		"    - matcher: Bash\n" +
		"esheep-targets: [pi]\n" +
		"x-owner: engineering\n" +
		"---\nbody")
	document, err := Parse(input, "demo", "SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if !document.DisableModelInvocation {
		t.Fatal("DisableModelInvocation = false, want true")
	}
	if !document.Targets.Pi.Listed || document.Targets.Claude.Listed {
		t.Fatalf("targets = %#v, want only pi listed", document.Targets)
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
		{name: "duplicate common key", dir: "demo", yaml: "name: demo\nname: demo\nesheep-trigger: ok\nesheep-targets: [claude]\n", code: CodeYAML},
		{name: "duplicate uninterpreted key", dir: "demo", yaml: "name: demo\nesheep-trigger: ok\nesheep-targets: [claude]\nextra: 1\nextra: 2\n", code: CodeInvalidValue},
		{name: "unknown esheep key", dir: "demo", yaml: "name: demo\nesheep-trigger: ok\nesheep-targets: [claude]\nesheep-only-layer: [work]\n", code: CodeUnknownField},
		{name: "invocation toggle is string", dir: "demo", yaml: "name: demo\nesheep-trigger: ok\nesheep-targets: [claude]\ndisable-model-invocation: 'yes'\n", code: CodeInvalidValue},
		{name: "disabled toggle is string", dir: "demo", yaml: "name: demo\nesheep-trigger: ok\nesheep-targets: [claude]\nesheep-disabled: 'true'\n", code: CodeInvalidValue},
		{name: "disabled toggle is null", dir: "demo", yaml: "name: demo\nesheep-trigger: ok\nesheep-targets: [claude]\nesheep-disabled: null\n", code: CodeInvalidValue},
		{name: "disabled manifest requires targets", dir: "demo", yaml: "name: demo\nesheep-trigger: ok\nesheep-disabled: true\n", code: CodeRequiredField},
		{name: "name is map", dir: "demo", yaml: "name: {}\nesheep-trigger: ok\nesheep-targets: [claude]\n", code: CodeInvalidValue},
		{name: "license is boolean", dir: "demo", yaml: "name: demo\nesheep-trigger: ok\nesheep-targets: [claude]\nlicense: true\n", code: CodeInvalidValue},
		{name: "metadata is scalar", dir: "demo", yaml: "name: demo\nesheep-trigger: ok\nesheep-targets: [claude]\nmetadata: no\n", code: CodeInvalidValue},
		{name: "metadata value type", dir: "demo", yaml: "name: demo\nesheep-trigger: ok\nesheep-targets: [claude]\nmetadata:\n  count: 2\n", code: CodeInvalidValue},
		{name: "targets missing", dir: "demo", yaml: "name: demo\nesheep-trigger: ok\n", code: CodeRequiredField},
		{name: "targets is scalar", dir: "demo", yaml: "name: demo\nesheep-trigger: ok\nesheep-targets: claude\n", code: CodeInvalidValue},
		{name: "targets is empty", dir: "demo", yaml: "name: demo\nesheep-trigger: ok\nesheep-targets: []\n", code: CodeInvalidValue},
		{name: "target name is empty", dir: "demo", yaml: "name: demo\nesheep-trigger: ok\nesheep-targets: ['']\n", code: CodeInvalidValue},
		{name: "targets unknown name", dir: "demo", yaml: "name: demo\nesheep-trigger: ok\nesheep-targets: [opencode]\n", code: CodeInvalidValue},
		{name: "targets item is number", dir: "demo", yaml: "name: demo\nesheep-trigger: ok\nesheep-targets: [3]\n", code: CodeInvalidValue},
		{name: "targets duplicate", dir: "demo", yaml: "name: demo\nesheep-trigger: ok\nesheep-targets: [claude, claude]\n", code: CodeInvalidValue},
		{name: "targets entry multiple pairs", dir: "demo", yaml: "name: demo\nesheep-trigger: ok\nesheep-targets:\n  - claude: [work]\n    pi: [work]\n", code: CodeInvalidValue},
		{name: "target gate is scalar", dir: "demo", yaml: "name: demo\nesheep-trigger: ok\nesheep-targets: [claude: work]\n", code: CodeInvalidValue},
		{name: "target gate is empty", dir: "demo", yaml: "name: demo\nesheep-trigger: ok\nesheep-targets: [claude: []]\n", code: CodeInvalidValue},
		{name: "target gate item is number", dir: "demo", yaml: "name: demo\nesheep-trigger: ok\nesheep-targets: [claude: [3]]\n", code: CodeInvalidValue},
		{name: "target gate bad grammar", dir: "demo", yaml: "name: demo\nesheep-trigger: ok\nesheep-targets: [claude: [Work]]\n", code: CodeInvalidLayer},
		{name: "target gate reserved name", dir: "demo", yaml: "name: demo\nesheep-trigger: ok\nesheep-targets: [claude: [base]]\n", code: CodeInvalidLayer},
		{name: "target gate duplicate", dir: "demo", yaml: "name: demo\nesheep-trigger: ok\nesheep-targets: [claude: [work, work]]\n", code: CodeInvalidValue},
		{name: "only-layers is scalar", dir: "demo", yaml: "name: demo\nesheep-trigger: ok\nesheep-targets: [claude]\nesheep-only-layers: work\n", code: CodeInvalidValue},
		{name: "only-layers is empty", dir: "demo", yaml: "name: demo\nesheep-trigger: ok\nesheep-targets: [claude]\nesheep-only-layers: []\n", code: CodeInvalidValue},
		{name: "only-layers item is number", dir: "demo", yaml: "name: demo\nesheep-trigger: ok\nesheep-targets: [claude]\nesheep-only-layers: [3]\n", code: CodeInvalidValue},
		{name: "only-layers bad grammar", dir: "demo", yaml: "name: demo\nesheep-trigger: ok\nesheep-targets: [claude]\nesheep-only-layers: [Work]\n", code: CodeInvalidLayer},
		{name: "only-layers reserved name", dir: "demo", yaml: "name: demo\nesheep-trigger: ok\nesheep-targets: [claude]\nesheep-only-layers: [base]\n", code: CodeInvalidLayer},
		{name: "only-layers duplicate", dir: "demo", yaml: "name: demo\nesheep-trigger: ok\nesheep-targets: [claude]\nesheep-only-layers: [work, work]\n", code: CodeInvalidValue},
		{name: "invalid name", dir: "bad_name", yaml: "name: bad_name\nesheep-trigger: ok\nesheep-targets: [claude]\n", code: CodeInvalidName},
		{name: "directory mismatch", dir: "other", yaml: "name: demo\nesheep-trigger: ok\nesheep-targets: [claude]\n", code: CodeNameMismatch},
		{name: "blank trigger", dir: "demo", yaml: "name: demo\nesheep-trigger: '  '\nesheep-targets: [claude]\n", code: CodeRequiredField},
		{name: "missing trigger", dir: "demo", yaml: "name: demo\nesheep-targets: [claude]\n", code: CodeRequiredField},
		{name: "trigger is number", dir: "demo", yaml: "name: demo\nesheep-trigger: 12\nesheep-targets: [claude]\n", code: CodeInvalidValue},
		{name: "trigger is null", dir: "demo", yaml: "name: demo\nesheep-trigger: null\nesheep-targets: [claude]\n", code: CodeInvalidValue},
		{name: "trigger is mapping", dir: "demo", yaml: "name: demo\nesheep-trigger: {}\nesheep-targets: [claude]\n", code: CodeInvalidValue},
		{name: "long trigger", dir: "demo", yaml: "name: demo\nesheep-trigger: " + strings.Repeat("界", 1025) + "\nesheep-targets: [claude]\n", code: CodeInvalidValue},
		{name: "long compatibility", dir: "demo", yaml: "name: demo\nesheep-trigger: ok\nesheep-targets: [claude]\ncompatibility: " + strings.Repeat("界", 501) + "\n", code: CodeInvalidValue},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := []byte(test.yaml)
			if test.code != CodeFrontmatter {
				input = []byte("---\n" + test.yaml + "---\nbody")
			}
			_, err := Parse(input, test.dir, "SKILL.md")
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

func TestParseRejectsRenderedDescription(t *testing.T) {
	t.Parallel()
	tests := []struct {
		fields string
		key    string
		name   string
	}{
		{name: "description alone", key: "description", fields: "description: invoke when asked\n"},
		{name: "both fields", key: "description", fields: "esheep-trigger: invoke when asked\ndescription: conflicting text\n"},
		{name: "empty description", key: "description", fields: "esheep-trigger: invoke when asked\ndescription: ''\n"},
		{name: "null description", key: "description", fields: "esheep-trigger: invoke when asked\ndescription: null\n"},
		{name: "structured description", key: "description", fields: "esheep-trigger: invoke when asked\ndescription: {text: invoke}\n"},
		{name: "title case description", key: "Description", fields: "esheep-trigger: invoke when asked\nDescription: conflicting text\n"},
		{name: "uppercase description", key: "DESCRIPTION", fields: "esheep-trigger: invoke when asked\nDESCRIPTION: conflicting text\n"},
		{name: "mixed case description", key: "dEsCrIpTiOn", fields: "esheep-trigger: invoke when asked\ndEsCrIpTiOn: conflicting text\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := []byte("---\nname: demo\nesheep-targets: [claude]\n" + test.fields + "---\nbody\n")

			document, err := Parse(input, "demo", "SKILL.md")

			diagnostics := ErrorDiagnostics(err)
			if !slices.ContainsFunc(diagnostics, func(diagnostic Diagnostic) bool {
				return diagnostic.Code == CodeUnknownField && diagnostic.Field == test.key && diagnostic.Path == "SKILL.md"
			}) {
				t.Fatalf("Parse diagnostics = %#v, want rejected rendered field", diagnostics)
			}
			if len(document.Extra) != 0 {
				t.Fatalf("Extra = %#v, want no rendered field passed through", document.Extra)
			}
		})
	}
}

func TestParsePreservesDescriptionOutsideTopLevelFields(t *testing.T) {
	t.Parallel()
	const body = "Explain the description field.\n"
	input := []byte("---\nname: demo\nesheep-trigger: invoke when asked\nesheep-targets: [claude]\ncustom:\n  description: nested text\n---\n" + body)

	document, err := Parse(input, "demo", "SKILL.md")

	if err != nil {
		t.Fatal(err)
	}
	if string(document.Body) != body {
		t.Fatalf("body = %q, want %q", document.Body, body)
	}
	if len(document.Extra) != 1 || document.Extra[0].Key != "custom" {
		t.Fatalf("Extra = %#v, want custom field", document.Extra)
	}
	var custom map[string]string
	if err := document.Extra[0].Value.Decode(&custom); err != nil {
		t.Fatal(err)
	}
	if custom["description"] != "nested text" {
		t.Fatalf("custom = %#v, want nested description preserved", custom)
	}
}

func TestLoadFollowsCrossRepositorySymlinks(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	overlay := filepath.Join(parent, "overlay", "demo")
	if err := os.MkdirAll(filepath.Join(overlay, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overlay, "SKILL.md"), []byte("---\nname: demo\nesheep-trigger: ok\nesheep-targets: [claude]\n---\nbody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overlay, "scripts", "run.sh"), []byte("echo overlay\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "source", "demo")
	if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../overlay/demo/SKILL.md", filepath.Join(root, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../../overlay/demo/scripts/run.sh", filepath.Join(root, "scripts", "run.sh")); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}

	if len(loaded.Manifests) != 1 || loaded.Manifests[0].Document.Name != "demo" {
		t.Fatalf("manifests = %#v", loaded.Manifests)
	}
	if len(loaded.Files) != 1 || loaded.Files[0].Path != "scripts/run.sh" {
		t.Fatalf("files = %#v", loaded.Files)
	}
}

func TestLoadSkipsHiddenEntriesBeforeValidation(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "demo")
	for _, directory := range []string{".git", "support/.cache"} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeLoadManifest(t, root)
	for _, directory := range []string{".", "support"} {
		for _, name := range []string{".env", ".DS_Store"} {
			if err := os.WriteFile(filepath.Join(root, directory, name), []byte("hidden"), 0); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(root, directory, "guide.v1.md"), []byte("visible"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Mkfifo(filepath.Join(root, directory, ".pipe"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, target := range map[string]string{
		".broken":              "missing",
		".cycle":               ".",
		".git/broken":          "missing",
		"support/.broken":      "missing",
		"support/.cycle":       ".",
		"support/.cache/cycle": ".",
	} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}

	loaded, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{"support"}; !slices.Equal(loaded.Directories, want) {
		t.Errorf("Load().Directories = %#v, want %#v", loaded.Directories, want)
	}
	if want := []File{{Path: "guide.v1.md"}, {Path: "support/guide.v1.md"}}; !slices.Equal(loaded.Files, want) {
		t.Errorf("Load().Files = %#v, want %#v", loaded.Files, want)
	}
}

func TestLoadTraversesVisibleSymlinksToHiddenTargets(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	shared := filepath.Join(parent, ".shared")
	if err := os.MkdirAll(filepath.Join(shared, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shared, "nested", "data"), []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shared, ".payload"), []byte("linked payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing", filepath.Join(shared, "nested", ".broken")); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "demo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("---\nname: demo\nesheep-trigger: ok\nesheep-targets: [claude]\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../.shared", filepath.Join(root, "reference")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../.shared/.payload", filepath.Join(root, "data")); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}

	wantDirectories := []string{"reference", "reference/nested"}
	if len(loaded.Directories) != len(wantDirectories) || loaded.Directories[0] != wantDirectories[0] || loaded.Directories[1] != wantDirectories[1] {
		t.Fatalf("directories = %#v, want %#v", loaded.Directories, wantDirectories)
	}
	if want := []File{{Path: "data"}, {Path: "reference/nested/data"}}; !slices.Equal(loaded.Files, want) {
		t.Errorf("Load().Files = %#v, want %#v", loaded.Files, want)
	}
}

func TestLoadReportsUnresolvableSymlinks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		build func(*testing.T, string)
		path  string
	}{
		{
			name: "dangling manifest",
			build: func(t *testing.T, root string) {
				t.Helper()
				if err := os.Symlink("missing", filepath.Join(root, "SKILL.md")); err != nil {
					t.Fatal(err)
				}
			},
			path: "SKILL.md",
		},
		{
			name: "dangling support file",
			build: func(t *testing.T, root string) {
				t.Helper()
				writeLoadManifest(t, root)
				if err := os.Symlink("missing", filepath.Join(root, "data")); err != nil {
					t.Fatal(err)
				}
			},
			path: "data",
		},
		{
			name: "self cycle",
			build: func(t *testing.T, root string) {
				t.Helper()
				writeLoadManifest(t, root)
				if err := os.Symlink("loop", filepath.Join(root, "loop")); err != nil {
					t.Fatal(err)
				}
			},
			path: "loop",
		},
		{
			name: "directory cycle",
			build: func(t *testing.T, root string) {
				t.Helper()
				writeLoadManifest(t, root)
				if err := os.Symlink(".", filepath.Join(root, "loop")); err != nil {
					t.Fatal(err)
				}
			},
			path: "loop",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := filepath.Join(t.TempDir(), "demo")
			if err := os.Mkdir(root, 0o755); err != nil {
				t.Fatal(err)
			}
			test.build(t, root)

			_, err := Load(root)

			diagnostics := ErrorDiagnostics(err)
			if len(diagnostics) != 1 || diagnostics[0].Code != CodeUnreadable || diagnostics[0].Path != test.path {
				t.Fatalf("diagnostics = %#v, want one unreadable at %q", diagnostics, test.path)
			}
		})
	}
}

func TestSourceOnlyEntriesRemainValidated(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "demo")
	directory := filepath.Join(root, "esheep-inputs")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLoadManifest(t, root)
	if err := os.Symlink("absent", filepath.Join(directory, "broken")); err != nil {
		t.Fatal(err)
	}

	_, err := Load(root)
	var invalid *ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("Load() error = %v, want invalid source-only input", err)
	}
	for _, diagnostic := range invalid.Diagnostics {
		if diagnostic.Path == "esheep-inputs/broken" && diagnostic.Code == CodeUnreadable {
			return
		}
	}
	t.Fatalf("diagnostics = %#v, want unreadable source-only path", invalid.Diagnostics)
}

func writeLoadManifest(t *testing.T, root string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("---\nname: demo\nesheep-trigger: ok\nesheep-targets: [claude]\n---\n"), 0o600); err != nil {
		t.Fatal(err)
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
	result := make(chan error, 1)
	go func() {
		result <- validateReadableRegular(filepath.Join(rootPath, "support"))
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
	input := "---\nname: demo\nesheep-trigger: " + strings.Repeat("界", 1024) + "\ncompatibility: " + strings.Repeat("界", 500) + "\nesheep-targets: [claude]\n---\n"
	if _, err := Parse([]byte(input), "demo", "SKILL.md"); err != nil {
		t.Fatal(err)
	}
}

func TestParseManifestNameClassifiesRootFiles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		fileName    string
		layer       string
		ok          bool
		invalidName bool
	}{
		{name: "base", fileName: "SKILL.md", layer: "", ok: true},
		{name: "variant", fileName: "SKILL.work.md", layer: "work", ok: true},
		{name: "hyphenated variant", fileName: "SKILL.work-laptop.md", layer: "work-laptop", ok: true},
		{name: "local variant", fileName: "SKILL.local.md", layer: "local", ok: true},
		{name: "supporting file", fileName: "notes.work.md", ok: false},
		{name: "lowercase stem", fileName: "skill.work.md", ok: false},
		{name: "extra segments", fileName: "SKILL.work.extra.md", ok: false},
		{name: "uppercase layer", fileName: "SKILL.Work.md", invalidName: true},
		{name: "empty layer", fileName: "SKILL..md", invalidName: true},
		{name: "reserved layer", fileName: "SKILL.base.md", invalidName: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			layer, ok, err := ParseManifestName(test.fileName)
			if test.invalidName {
				if err == nil {
					t.Fatalf("ParseManifestName(%q) accepted an invalid layer", test.fileName)
				}
				return
			}
			if err != nil || ok != test.ok || layer != test.layer {
				t.Fatalf("ParseManifestName(%q) = %q, %v, %v, want %q, %v, nil", test.fileName, layer, ok, err, test.layer, test.ok)
			}
		})
	}
}

func TestSelectFollowsLayerOrder(t *testing.T) {
	t.Parallel()
	base := Manifest{FileName: "SKILL.md"}
	gatedBase := Manifest{FileName: "SKILL.md", Document: Document{OnlyLayers: []string{"client"}}}
	work := Manifest{FileName: "SKILL.work.md", Layer: "work"}
	client := Manifest{FileName: "SKILL.client.md", Layer: "client"}
	local := Manifest{FileName: "SKILL.local.md", Layer: "local"}
	narrowedWork := Manifest{FileName: "SKILL.work.md", Layer: "work", Document: Document{OnlyLayers: []string{"laptop"}}}
	tests := []struct {
		name      string
		manifests []Manifest
		layers    []string
		selected  string
	}{
		{name: "base applies without layers", manifests: []Manifest{base}, selected: "SKILL.md"},
		{name: "base applies under any layer", manifests: []Manifest{base}, layers: []string{"work"}, selected: "SKILL.md"},
		{name: "gated base inactive", manifests: []Manifest{gatedBase}, layers: []string{"work"}},
		{name: "gated base active", manifests: []Manifest{gatedBase}, layers: []string{"client"}, selected: "SKILL.md"},
		{name: "variant inactive without layer", manifests: []Manifest{work}},
		{name: "variant beats base", manifests: []Manifest{base, work}, layers: []string{"work"}, selected: "SKILL.work.md"},
		{name: "inactive variant leaves base", manifests: []Manifest{base, work}, layers: []string{"client"}, selected: "SKILL.md"},
		{name: "latest effective layer wins", manifests: []Manifest{base, client, work}, layers: []string{"client", "work"}, selected: "SKILL.work.md"},
		{name: "effective order decides not file order", manifests: []Manifest{base, client, work}, layers: []string{"work", "client"}, selected: "SKILL.client.md"},
		{name: "local applies without layers", manifests: []Manifest{base, local}, selected: "SKILL.local.md"},
		{name: "local alone applies", manifests: []Manifest{local}, selected: "SKILL.local.md"},
		{name: "local wins over effective layers", manifests: []Manifest{base, work, local}, layers: []string{"work"}, selected: "SKILL.local.md"},
		{name: "gated local inactive", manifests: []Manifest{base, {FileName: "SKILL.local.md", Layer: "local", Document: Document{OnlyLayers: []string{"work"}}}}, selected: "SKILL.md"},
		{name: "narrowed variant inactive under gate alone", manifests: []Manifest{narrowedWork}, layers: []string{"laptop"}},
		{name: "narrowed variant inactive under filename layer alone", manifests: []Manifest{narrowedWork}, layers: []string{"work"}},
		{name: "narrowed variant active under both", manifests: []Manifest{narrowedWork}, layers: []string{"work", "laptop"}, selected: "SKILL.work.md"},
		{name: "narrowed variant keeps filename position", manifests: []Manifest{narrowedWork, client}, layers: []string{"work", "client", "laptop"}, selected: "SKILL.client.md"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			selection := Package{Manifests: test.manifests}.Select(test.layers)
			if selection.Active != (test.selected != "") {
				t.Fatalf("Select(%v).Active = %v, want %v", test.layers, selection.Active, test.selected != "")
			}
			if selection.Manifest.FileName != test.selected {
				t.Errorf("Select(%v) selected %q, want %q", test.layers, selection.Manifest.FileName, test.selected)
			}
		})
	}
}

func TestGateAndReferencedLayersUnionManifestGates(t *testing.T) {
	t.Parallel()
	universal := Package{Manifests: []Manifest{
		{FileName: "SKILL.md"},
		{FileName: "SKILL.work.md", Layer: "work"},
	}}
	gated := Package{Manifests: []Manifest{
		{FileName: "SKILL.md", Document: Document{OnlyLayers: []string{"client"}}},
		{FileName: "SKILL.work.md", Layer: "work"},
	}}
	invalid := Package{Manifests: []Manifest{
		{FileName: "SKILL.md", Document: Document{OnlyLayers: []string{"Work", "client"}}},
	}}
	localOnly := Package{Manifests: []Manifest{
		{FileName: "SKILL.local.md", Layer: "local"},
	}}
	localGated := Package{Manifests: []Manifest{
		{FileName: "SKILL.local.md", Layer: "local", Document: Document{OnlyLayers: []string{"work"}}},
	}}

	if gate := universal.Gate(); gate != nil {
		t.Errorf("universal Gate() = %v, want nil", gate)
	}
	if referenced := universal.ReferencedLayers(); len(referenced) != 1 || referenced[0] != "work" {
		t.Errorf("universal ReferencedLayers() = %v, want [work]", referenced)
	}
	gate := gated.Gate()
	if len(gate) != 2 || gate[0] != "client" || gate[1] != "work" {
		t.Errorf("gated Gate() = %v, want [client work]", gate)
	}
	if referenced := invalid.ReferencedLayers(); len(referenced) != 1 || referenced[0] != "client" {
		t.Errorf("invalid ReferencedLayers() = %v, want [client]", referenced)
	}
	if gate := localOnly.Gate(); gate != nil {
		t.Errorf("local-only Gate() = %v, want nil", gate)
	}
	if referenced := localOnly.ReferencedLayers(); referenced != nil {
		t.Errorf("local-only ReferencedLayers() = %v, want nil", referenced)
	}
	if gate := localGated.Gate(); len(gate) != 1 || gate[0] != "work" {
		t.Errorf("local gated Gate() = %v, want [work]", gate)
	}
}

func TestReferencedLayersIncludeTargetGates(t *testing.T) {
	t.Parallel()
	source := Package{Manifests: []Manifest{{
		FileName: "SKILL.md",
		Document: Document{Targets: Targets{
			Claude: TargetOptions{Listed: true, OnlyLayers: []string{"work", "Bad"}},
			Pi:     TargetOptions{Listed: true},
		}},
	}}}

	referenced := source.ReferencedLayers()

	if len(referenced) != 1 || referenced[0] != "work" {
		t.Fatalf("ReferencedLayers() = %v, want [work]", referenced)
	}
	if gate := source.Gate(); gate != nil {
		t.Fatalf("Gate() = %v, want nil for an ungated manifest", gate)
	}
}

func TestTargetOptionsAppliesHonorsLayerGate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		options TargetOptions
		layers  []string
		want    bool
	}{
		{name: "unlisted never applies", options: TargetOptions{}, layers: []string{"work"}, want: false},
		{name: "ungated applies without layers", options: TargetOptions{Listed: true}, want: true},
		{name: "ungated applies under any layer", options: TargetOptions{Listed: true}, layers: []string{"personal"}, want: true},
		{name: "gated needs a matching layer", options: TargetOptions{Listed: true, OnlyLayers: []string{"work"}}, want: false},
		{name: "gated rejects other layers", options: TargetOptions{Listed: true, OnlyLayers: []string{"work"}}, layers: []string{"personal"}, want: false},
		{name: "gated matches an active layer", options: TargetOptions{Listed: true, OnlyLayers: []string{"work"}}, layers: []string{"work"}, want: true},
		{name: "any active layer matches", options: TargetOptions{Listed: true, OnlyLayers: []string{"work", "client"}}, layers: []string{"client"}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := test.options.Applies(test.layers); got != test.want {
				t.Fatalf("Applies(%v) = %v, want %v", test.layers, got, test.want)
			}
		})
	}
}

func TestLoadReadsLayerVariantManifests(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "demo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeManifest := func(name, trigger string) {
		content := "---\nname: demo\nesheep-trigger: " + trigger + "\nesheep-targets: [claude]\n---\nbody\n"
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeManifest("SKILL.local.md", "local variant")
	writeManifest("SKILL.work.md", "work variant")
	writeManifest("SKILL.client.md", "client variant")
	writeManifest("SKILL.md", "base")
	if err := os.WriteFile(filepath.Join(root, "reference.md"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}

	var order []string
	for _, manifest := range loaded.Manifests {
		order = append(order, manifest.FileName+":"+manifest.Layer)
	}
	want := []string{"SKILL.md:", "SKILL.client.md:client", "SKILL.work.md:work", "SKILL.local.md:local"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("manifest order = %v, want %v", order, want)
	}

	if len(loaded.Files) != 1 || loaded.Files[0].Path != "reference.md" {
		t.Fatalf("files = %#v, want only reference.md", loaded.Files)
	}
}

func TestLoadRejectsInvalidVariantLayerSegment(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "demo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "SKILL.Work.md"), []byte("---\nname: demo\nesheep-trigger: ok\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(root)

	diagnostics := ErrorDiagnostics(err)
	if len(diagnostics) != 1 || diagnostics[0].Code != CodeInvalidLayer || diagnostics[0].Path != "SKILL.Work.md" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestLoadReportsVariantDiagnosticsUnderVariantPath(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "demo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("---\nname: demo\nesheep-trigger: ok\nesheep-targets: [claude]\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "SKILL.work.md"), []byte("---\nname: demo\nesheep-targets: [claude]\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(root)

	diagnostics := ErrorDiagnostics(err)
	if len(diagnostics) != 1 || diagnostics[0].Code != CodeRequiredField || diagnostics[0].Path != "SKILL.work.md" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}
