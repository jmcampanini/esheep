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
	input := append([]byte("---\r\nname: demo-skill\r\ndescription: ' useful '\r\nlicense: ''\r\ncompatibility: ''\r\nmetadata: {}\r\nesheep-targets: [claude, pi: [work]]\r\nesheep-only-profiles: [work]\r\n---\r\n"), body...)
	document, err := Parse(input, "demo-skill", "SKILL.md")
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
	if !document.Targets.Claude.Listed || !document.Targets.Pi.Listed || document.Targets.Codex.Listed || document.Targets.Agents.Listed {
		t.Fatalf("targets = %#v", document.Targets)
	}
	if document.Targets.Claude.OnlyProfiles != nil {
		t.Fatalf("claude gate = %#v, want none", document.Targets.Claude.OnlyProfiles)
	}
	if len(document.Targets.Pi.OnlyProfiles) != 1 || document.Targets.Pi.OnlyProfiles[0] != "work" {
		t.Fatalf("pi gate = %#v, want [work]", document.Targets.Pi.OnlyProfiles)
	}
	if len(document.OnlyProfiles) != 1 || document.OnlyProfiles[0] != "work" {
		t.Fatalf("only profiles = %#v, want [work]", document.OnlyProfiles)
	}
}

func TestParseDistinguishesAbsentOptionalFields(t *testing.T) {
	t.Parallel()
	document, err := Parse([]byte("---\nname: demo\ndescription: ok\nesheep-targets: [claude]\n---\n"), "demo", "SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if document.License != nil || document.Compatibility != nil || document.Metadata != nil || document.OnlyProfiles != nil {
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
		{name: "duplicate common key", dir: "demo", yaml: "name: demo\nname: demo\ndescription: ok\nesheep-targets: [claude]\n", code: CodeYAML},
		{name: "duplicate uninterpreted key", dir: "demo", yaml: "name: demo\ndescription: ok\nesheep-targets: [claude]\nextra: 1\nextra: 2\n", code: CodeInvalidValue},
		{name: "unknown esheep key", dir: "demo", yaml: "name: demo\ndescription: ok\nesheep-targets: [claude]\nesheep-only-profile: [work]\n", code: CodeUnknownField},
		{name: "invocation toggle is string", dir: "demo", yaml: "name: demo\ndescription: ok\nesheep-targets: [claude]\ndisable-model-invocation: 'yes'\n", code: CodeInvalidValue},
		{name: "name is map", dir: "demo", yaml: "name: {}\ndescription: ok\nesheep-targets: [claude]\n", code: CodeInvalidValue},
		{name: "license is boolean", dir: "demo", yaml: "name: demo\ndescription: ok\nesheep-targets: [claude]\nlicense: true\n", code: CodeInvalidValue},
		{name: "metadata is scalar", dir: "demo", yaml: "name: demo\ndescription: ok\nesheep-targets: [claude]\nmetadata: no\n", code: CodeInvalidValue},
		{name: "metadata value type", dir: "demo", yaml: "name: demo\ndescription: ok\nesheep-targets: [claude]\nmetadata:\n  count: 2\n", code: CodeInvalidValue},
		{name: "targets missing", dir: "demo", yaml: "name: demo\ndescription: ok\n", code: CodeRequiredField},
		{name: "targets is scalar", dir: "demo", yaml: "name: demo\ndescription: ok\nesheep-targets: claude\n", code: CodeInvalidValue},
		{name: "targets is empty", dir: "demo", yaml: "name: demo\ndescription: ok\nesheep-targets: []\n", code: CodeInvalidValue},
		{name: "target name is empty", dir: "demo", yaml: "name: demo\ndescription: ok\nesheep-targets: ['']\n", code: CodeInvalidValue},
		{name: "targets unknown name", dir: "demo", yaml: "name: demo\ndescription: ok\nesheep-targets: [opencode]\n", code: CodeInvalidValue},
		{name: "targets item is number", dir: "demo", yaml: "name: demo\ndescription: ok\nesheep-targets: [3]\n", code: CodeInvalidValue},
		{name: "targets duplicate", dir: "demo", yaml: "name: demo\ndescription: ok\nesheep-targets: [claude, claude]\n", code: CodeInvalidValue},
		{name: "targets entry multiple pairs", dir: "demo", yaml: "name: demo\ndescription: ok\nesheep-targets:\n  - claude: [work]\n    pi: [work]\n", code: CodeInvalidValue},
		{name: "target gate is scalar", dir: "demo", yaml: "name: demo\ndescription: ok\nesheep-targets: [claude: work]\n", code: CodeInvalidValue},
		{name: "target gate is empty", dir: "demo", yaml: "name: demo\ndescription: ok\nesheep-targets: [claude: []]\n", code: CodeInvalidValue},
		{name: "target gate item is number", dir: "demo", yaml: "name: demo\ndescription: ok\nesheep-targets: [claude: [3]]\n", code: CodeInvalidValue},
		{name: "target gate bad grammar", dir: "demo", yaml: "name: demo\ndescription: ok\nesheep-targets: [claude: [Work]]\n", code: CodeInvalidProfile},
		{name: "target gate reserved name", dir: "demo", yaml: "name: demo\ndescription: ok\nesheep-targets: [claude: [base]]\n", code: CodeInvalidProfile},
		{name: "target gate duplicate", dir: "demo", yaml: "name: demo\ndescription: ok\nesheep-targets: [claude: [work, work]]\n", code: CodeInvalidValue},
		{name: "only-profiles is scalar", dir: "demo", yaml: "name: demo\ndescription: ok\nesheep-targets: [claude]\nesheep-only-profiles: work\n", code: CodeInvalidValue},
		{name: "only-profiles is empty", dir: "demo", yaml: "name: demo\ndescription: ok\nesheep-targets: [claude]\nesheep-only-profiles: []\n", code: CodeInvalidValue},
		{name: "only-profiles item is number", dir: "demo", yaml: "name: demo\ndescription: ok\nesheep-targets: [claude]\nesheep-only-profiles: [3]\n", code: CodeInvalidValue},
		{name: "only-profiles bad grammar", dir: "demo", yaml: "name: demo\ndescription: ok\nesheep-targets: [claude]\nesheep-only-profiles: [Work]\n", code: CodeInvalidProfile},
		{name: "only-profiles reserved name", dir: "demo", yaml: "name: demo\ndescription: ok\nesheep-targets: [claude]\nesheep-only-profiles: [base]\n", code: CodeInvalidProfile},
		{name: "only-profiles duplicate", dir: "demo", yaml: "name: demo\ndescription: ok\nesheep-targets: [claude]\nesheep-only-profiles: [work, work]\n", code: CodeInvalidValue},
		{name: "invalid name", dir: "bad_name", yaml: "name: bad_name\ndescription: ok\nesheep-targets: [claude]\n", code: CodeInvalidName},
		{name: "directory mismatch", dir: "other", yaml: "name: demo\ndescription: ok\nesheep-targets: [claude]\n", code: CodeNameMismatch},
		{name: "blank description", dir: "demo", yaml: "name: demo\ndescription: '  '\nesheep-targets: [claude]\n", code: CodeRequiredField},
		{name: "long description", dir: "demo", yaml: "name: demo\ndescription: " + strings.Repeat("界", 1025) + "\nesheep-targets: [claude]\n", code: CodeInvalidValue},
		{name: "long compatibility", dir: "demo", yaml: "name: demo\ndescription: ok\nesheep-targets: [claude]\ncompatibility: " + strings.Repeat("界", 501) + "\n", code: CodeInvalidValue},
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

func TestLoadFollowsCrossRepositorySymlinks(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	overlay := filepath.Join(parent, "overlay", "demo")
	if err := os.MkdirAll(filepath.Join(overlay, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overlay, "SKILL.md"), []byte("---\nname: demo\ndescription: ok\nesheep-targets: [claude]\n---\nbody\n"), 0o600); err != nil {
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

func TestLoadTraversesDirectorySymlinks(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	shared := filepath.Join(parent, "shared")
	if err := os.MkdirAll(filepath.Join(shared, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shared, "nested", "data"), []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "demo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("---\nname: demo\ndescription: ok\nesheep-targets: [claude]\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../shared", filepath.Join(root, "reference")); err != nil {
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
	if len(loaded.Files) != 1 || loaded.Files[0].Path != "reference/nested/data" {
		t.Fatalf("files = %#v", loaded.Files)
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

func writeLoadManifest(t *testing.T, root string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("---\nname: demo\ndescription: ok\nesheep-targets: [claude]\n---\n"), 0o600); err != nil {
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
	input := "---\nname: demo\ndescription: " + strings.Repeat("界", 1024) + "\ncompatibility: " + strings.Repeat("界", 500) + "\nesheep-targets: [claude]\n---\n"
	if _, err := Parse([]byte(input), "demo", "SKILL.md"); err != nil {
		t.Fatal(err)
	}
}

func TestParseManifestNameClassifiesRootFiles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		fileName    string
		profile     string
		ok          bool
		invalidName bool
	}{
		{name: "base", fileName: "SKILL.md", profile: "", ok: true},
		{name: "variant", fileName: "SKILL.work.md", profile: "work", ok: true},
		{name: "hyphenated variant", fileName: "SKILL.work-laptop.md", profile: "work-laptop", ok: true},
		{name: "supporting file", fileName: "notes.work.md", ok: false},
		{name: "lowercase stem", fileName: "skill.work.md", ok: false},
		{name: "extra segments", fileName: "SKILL.work.extra.md", ok: false},
		{name: "uppercase profile", fileName: "SKILL.Work.md", invalidName: true},
		{name: "empty profile", fileName: "SKILL..md", invalidName: true},
		{name: "reserved profile", fileName: "SKILL.base.md", invalidName: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			profile, ok, err := ParseManifestName(test.fileName)
			if test.invalidName {
				if err == nil {
					t.Fatalf("ParseManifestName(%q) accepted an invalid profile", test.fileName)
				}
				return
			}
			if err != nil || ok != test.ok || profile != test.profile {
				t.Fatalf("ParseManifestName(%q) = %q, %v, %v, want %q, %v, nil", test.fileName, profile, ok, err, test.profile, test.ok)
			}
		})
	}
}

func TestSelectResolvesManifestsAgainstActiveProfiles(t *testing.T) {
	t.Parallel()
	base := Manifest{FileName: "SKILL.md"}
	gatedBase := Manifest{FileName: "SKILL.md", Document: Document{OnlyProfiles: []string{"client"}}}
	work := Manifest{FileName: "SKILL.work.md", Profile: "work"}
	client := Manifest{FileName: "SKILL.client.md", Profile: "client"}
	tests := []struct {
		name      string
		manifests []Manifest
		profiles  []string
		active    bool
		selected  string
		conflicts []string
	}{
		{name: "base applies without profiles", manifests: []Manifest{base}, active: true, selected: "SKILL.md"},
		{name: "base applies under any profile", manifests: []Manifest{base}, profiles: []string{"work"}, active: true, selected: "SKILL.md"},
		{name: "gated base inactive", manifests: []Manifest{gatedBase}, profiles: []string{"work"}},
		{name: "gated base active", manifests: []Manifest{gatedBase}, profiles: []string{"client"}, active: true, selected: "SKILL.md"},
		{name: "variant inactive without profile", manifests: []Manifest{work}},
		{name: "variant beats base", manifests: []Manifest{base, work}, profiles: []string{"work"}, active: true, selected: "SKILL.work.md"},
		{name: "inactive variant leaves base", manifests: []Manifest{base, work}, profiles: []string{"client"}, active: true, selected: "SKILL.md"},
		{name: "two active variants conflict", manifests: []Manifest{base, client, work}, profiles: []string{"client", "work"}, conflicts: []string{"SKILL.client.md", "SKILL.work.md"}},
		{name: "frontmatter union activates variant", manifests: []Manifest{{FileName: "SKILL.work.md", Profile: "work", Document: Document{OnlyProfiles: []string{"client"}}}}, profiles: []string{"client"}, active: true, selected: "SKILL.work.md"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			selection := Package{Manifests: test.manifests}.Select(test.profiles)
			if selection.Active != test.active {
				t.Fatalf("Select(%v).Active = %v, want %v", test.profiles, selection.Active, test.active)
			}
			if test.active && selection.Manifest.FileName != test.selected {
				t.Fatalf("Select(%v) selected %q, want %q", test.profiles, selection.Manifest.FileName, test.selected)
			}
			if len(selection.Conflicts) != len(test.conflicts) {
				t.Fatalf("Select(%v).Conflicts = %v, want %v", test.profiles, selection.Conflicts, test.conflicts)
			}
			for index := range test.conflicts {
				if selection.Conflicts[index] != test.conflicts[index] {
					t.Fatalf("Select(%v).Conflicts = %v, want %v", test.profiles, selection.Conflicts, test.conflicts)
				}
			}
		})
	}
}

func TestGateAndReferencedProfilesUnionManifestGates(t *testing.T) {
	t.Parallel()
	universal := Package{Manifests: []Manifest{
		{FileName: "SKILL.md"},
		{FileName: "SKILL.work.md", Profile: "work"},
	}}
	gated := Package{Manifests: []Manifest{
		{FileName: "SKILL.md", Document: Document{OnlyProfiles: []string{"client"}}},
		{FileName: "SKILL.work.md", Profile: "work"},
	}}
	invalid := Package{Manifests: []Manifest{
		{FileName: "SKILL.md", Document: Document{OnlyProfiles: []string{"Work", "client"}}},
	}}

	if gate := universal.Gate(); gate != nil {
		t.Fatalf("universal Gate() = %v, want nil", gate)
	}
	if referenced := universal.ReferencedProfiles(); len(referenced) != 1 || referenced[0] != "work" {
		t.Fatalf("universal ReferencedProfiles() = %v, want [work]", referenced)
	}
	gate := gated.Gate()
	if len(gate) != 2 || gate[0] != "client" || gate[1] != "work" {
		t.Fatalf("gated Gate() = %v, want [client work]", gate)
	}
	if referenced := invalid.ReferencedProfiles(); len(referenced) != 1 || referenced[0] != "client" {
		t.Fatalf("invalid ReferencedProfiles() = %v, want [client]", referenced)
	}
}

func TestReferencedProfilesIncludeTargetGates(t *testing.T) {
	t.Parallel()
	source := Package{Manifests: []Manifest{{
		FileName: "SKILL.md",
		Document: Document{Targets: Targets{
			Claude: TargetOptions{Listed: true, OnlyProfiles: []string{"work", "Bad"}},
			Pi:     TargetOptions{Listed: true},
		}},
	}}}

	referenced := source.ReferencedProfiles()

	if len(referenced) != 1 || referenced[0] != "work" {
		t.Fatalf("ReferencedProfiles() = %v, want [work]", referenced)
	}
	if gate := source.Gate(); gate != nil {
		t.Fatalf("Gate() = %v, want nil for an ungated manifest", gate)
	}
}

func TestTargetOptionsAppliesHonorsProfileGate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		options  TargetOptions
		profiles []string
		want     bool
	}{
		{name: "unlisted never applies", options: TargetOptions{}, profiles: []string{"work"}, want: false},
		{name: "ungated applies without profiles", options: TargetOptions{Listed: true}, want: true},
		{name: "ungated applies under any profile", options: TargetOptions{Listed: true}, profiles: []string{"personal"}, want: true},
		{name: "gated needs a matching profile", options: TargetOptions{Listed: true, OnlyProfiles: []string{"work"}}, want: false},
		{name: "gated rejects other profiles", options: TargetOptions{Listed: true, OnlyProfiles: []string{"work"}}, profiles: []string{"personal"}, want: false},
		{name: "gated matches an active profile", options: TargetOptions{Listed: true, OnlyProfiles: []string{"work"}}, profiles: []string{"work"}, want: true},
		{name: "any active profile matches", options: TargetOptions{Listed: true, OnlyProfiles: []string{"work", "client"}}, profiles: []string{"client"}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := test.options.Applies(test.profiles); got != test.want {
				t.Fatalf("Applies(%v) = %v, want %v", test.profiles, got, test.want)
			}
		})
	}
}

func TestLoadReadsProfileVariantManifests(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "demo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeManifest := func(name, description string) {
		content := "---\nname: demo\ndescription: " + description + "\nesheep-targets: [claude]\n---\nbody\n"
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeManifest("SKILL.md", "base")
	writeManifest("SKILL.work.md", "work variant")
	if err := os.WriteFile(filepath.Join(root, "reference.md"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}

	if len(loaded.Manifests) != 2 {
		t.Fatalf("manifests = %#v, want base and work", loaded.Manifests)
	}
	if loaded.Manifests[0].FileName != "SKILL.md" || loaded.Manifests[0].Profile != "" {
		t.Fatalf("first manifest = %#v, want base first", loaded.Manifests[0])
	}
	if loaded.Manifests[1].FileName != "SKILL.work.md" || loaded.Manifests[1].Profile != "work" {
		t.Fatalf("second manifest = %#v, want the work variant", loaded.Manifests[1])
	}
	if len(loaded.Files) != 1 || loaded.Files[0].Path != "reference.md" {
		t.Fatalf("files = %#v, want only reference.md", loaded.Files)
	}
}

func TestLoadRejectsInvalidVariantProfileSegment(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "demo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "SKILL.Work.md"), []byte("---\nname: demo\ndescription: ok\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(root)

	diagnostics := ErrorDiagnostics(err)
	if len(diagnostics) != 1 || diagnostics[0].Code != CodeInvalidProfile || diagnostics[0].Path != "SKILL.Work.md" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestLoadReportsVariantDiagnosticsUnderVariantPath(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "demo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("---\nname: demo\ndescription: ok\nesheep-targets: [claude]\n---\n"), 0o600); err != nil {
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
