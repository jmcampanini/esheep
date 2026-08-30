package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

const expectedVersion = "e2e-test"

var (
	binaryPath string
	workDir    string
)

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	repositoryRoot, err := filepath.Abs("..")
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve repository root: %v\n", err)
		return 1
	}
	sandbox := filepath.Join(repositoryRoot, ".sandbox")
	if err := os.MkdirAll(sandbox, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "create sandbox: %v\n", err)
		return 1
	}
	workDir, err = os.MkdirTemp(sandbox, "e2e-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create e2e directory: %v\n", err)
		return 1
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	build := exec.Command("make", "VERSION="+expectedVersion, "BUILD_DIR="+workDir, "build")
	build.Dir = repositoryRoot
	build.Stdout = os.Stderr
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "build e2e binary: %v\n", err)
		return 1
	}
	binaryPath = filepath.Join(workDir, "esheep")
	return m.Run()
}

func TestMilestoneWorkflow(t *testing.T) {
	root := filepath.Join(workDir, "synchronization")
	home := filepath.Join(root, "home")
	configHome := filepath.Join(root, "config")
	personal := filepath.Join(root, "personal")
	work := filepath.Join(root, "work")
	claude := filepath.Join(root, "targets", "claude")
	pi := filepath.Join(root, "targets", "pi")
	codex := filepath.Join(root, "targets", "codex")
	agents := filepath.Join(root, "targets", "agents")
	for _, directory := range []string{home, configHome, personal, work} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeE2ESkill(t, personal, "alpha", "Alpha skill", "claude:\n  argument-hint: CLAUDE-ARG\npi:\n  argument-hint: PI-ARG\n", map[string]string{"data.txt": "alpha data"})
	writeE2ESkill(t, personal, "local-only", "Local skill", "disable-model-invocation: true\nallowed-tools: Bash\npi:\n  disabled: true\n", nil)
	writeE2ESkill(t, personal, "same", "Personal collision", "", nil)
	writeE2ESkill(t, personal, "unsafe", "Unsafe skill", "metadata: no\n", nil)
	writeE2ESkill(t, work, "beta", "Beta skill", "", nil)
	writeE2ESkill(t, work, "same", "Work collision", "", nil)
	settingsPath := filepath.Join(configHome, "esheep", "esheep.toml")
	writeSyncSettings(t, settingsPath, personal, work, claude, pi, codex, agents, true)
	environment := map[string]string{"HOME": home, "XDG_CONFIG_HOME": configHome}

	invalidSourcesBefore := snapshotTree(t, personal) + snapshotTree(t, work)
	invalidInventory := runEsheep(t, environment, "skills", "list", "--json")
	assertSuccess(t, invalidInventory)
	var invalidReport struct {
		Diagnostics []struct {
			Code    string `json:"code"`
			Field   string `json:"field"`
			Message string `json:"message"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(invalidInventory.stdout), &invalidReport); err != nil {
		t.Fatalf("decode invalid inventory: %v\n%s", err, invalidInventory.stdout)
	}
	var foundCollision, foundInvalidField bool
	for _, diagnostic := range invalidReport.Diagnostics {
		if diagnostic.Code == "invalid-value" && diagnostic.Field == "metadata" {
			foundInvalidField = true
		}
		if diagnostic.Code == "collision" && strings.Contains(diagnostic.Message, "personal/same") && strings.Contains(diagnostic.Message, "work/same") {
			foundCollision = true
		}
	}
	if !foundCollision || !foundInvalidField {
		t.Fatalf("invalid inventory diagnostics = %#v", invalidReport.Diagnostics)
	}
	failedSync := runEsheep(t, environment, "sync")
	if failedSync.exitCode != 1 || !strings.Contains(failedSync.stderr, "invalid-value") || !strings.Contains(failedSync.stderr, "collision") {
		t.Fatalf("invalid sync result = %#v", failedSync)
	}
	if got := snapshotTree(t, personal) + snapshotTree(t, work); got != invalidSourcesBefore {
		t.Fatal("validation or failed synchronization modified a source directory")
	}

	for _, path := range []string{filepath.Join(personal, "unsafe"), filepath.Join(work, "same"), claude, pi, codex, agents} {
		if err := os.RemoveAll(path); err != nil {
			t.Fatal(err)
		}
	}
	sourcesBefore := snapshotTree(t, personal) + snapshotTree(t, work)
	settingsBefore := snapshotTree(t, configHome)

	configuration := runEsheep(t, environment, "config")
	assertSuccess(t, configuration)
	listed := runEsheep(t, environment, "skills", "list", "--json")
	assertSuccess(t, listed)
	var inventory struct {
		Skills []struct {
			Directory string `json:"directory"`
			Source    string `json:"source"`
		} `json:"skills"`
	}
	if err := json.Unmarshal([]byte(listed.stdout), &inventory); err != nil || len(inventory.Skills) != 4 {
		t.Fatalf("inventory: %v %#v\n%s", err, inventory, listed.stdout)
	}
	first := runEsheep(t, environment, "sync")
	assertSuccess(t, first)
	if !strings.Contains(first.stdout, "installed") || !strings.Contains(first.stdout, "failed=0") {
		t.Fatalf("initial sync stdout = %s", first.stdout)
	}
	status := runEsheep(t, environment, "skills", "status", "--json")
	assertSuccess(t, status)
	assertStatusHealth(t, status.stdout, true)
	if got := snapshotTree(t, personal) + snapshotTree(t, work); got != sourcesBefore {
		t.Fatal("representative workflow modified a source directory")
	}
	if got := snapshotTree(t, configHome); got != settingsBefore {
		t.Fatal("representative workflow modified settings")
	}
	for _, target := range []struct {
		name string
		path string
	}{
		{name: "claude", path: claude},
		{name: "pi", path: pi},
		{name: "codex", path: codex},
		{name: "agents", path: agents},
	} {
		for _, identity := range []struct {
			skill  string
			source string
		}{
			{skill: "alpha", source: "personal"},
			{skill: "beta", source: "work"},
			{skill: "same", source: "personal"},
		} {
			assertMarker(t, target.path, target.name, identity.source, identity.skill)
		}
	}
	assertMarker(t, claude, "claude", "personal", "local-only")
	assertMarker(t, codex, "codex", "personal", "local-only")
	assertMarker(t, agents, "agents", "personal", "local-only")
	if _, err := os.Stat(filepath.Join(pi, "local-only")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Pi received disabled skill: %v", err)
	}
	if info, err := os.Stat(filepath.Join(claude, "alpha", "data.txt")); err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("supporting file mode: info=%v err=%v", info, err)
	}
	assertManifestMetadata(t, filepath.Join(claude, "alpha", "SKILL.md"), "argument-hint: CLAUDE-ARG", true)
	assertManifestMetadata(t, filepath.Join(pi, "alpha", "SKILL.md"), "argument-hint: PI-ARG", true)
	assertManifestMetadata(t, filepath.Join(codex, "alpha", "SKILL.md"), "argument-hint:", false)
	assertManifestMetadata(t, filepath.Join(agents, "alpha", "SKILL.md"), "argument-hint:", false)
	assertManifestMetadata(t, filepath.Join(claude, "local-only", "SKILL.md"), "disable-model-invocation: true", true)
	assertManifestMetadata(t, filepath.Join(claude, "local-only", "SKILL.md"), "allowed-tools: Bash", true)
	assertManifestMetadata(t, filepath.Join(agents, "local-only", "SKILL.md"), "allowed-tools: Bash", true)
	codexPolicy, err := os.ReadFile(filepath.Join(codex, "local-only", "agents", "openai.yaml"))
	if err != nil || string(codexPolicy) != "policy:\n  allow_implicit_invocation: false\n" {
		t.Fatalf("codex invocation policy = %q, %v", codexPolicy, err)
	}
	if _, err := os.Stat(filepath.Join(claude, "local-only", "agents")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("claude received the codex invocation policy: %v", err)
	}

	if err := os.WriteFile(filepath.Join(claude, "alpha", "SKILL.md"), []byte("drift"), 0o644); err != nil {
		t.Fatal(err)
	}
	drifted := runEsheep(t, environment, "skills", "status", "--json")
	if drifted.exitCode != 1 || drifted.stderr != "" {
		t.Fatalf("drifted status = %#v", drifted)
	}
	assertStatusHealth(t, drifted.stdout, false)
	if err := os.Mkdir(filepath.Join(claude, "human-owned"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claude, "human-owned", "keep"), []byte("human"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codex, "alpha", "disabled-target-sentinel"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(work, "beta")); err != nil {
		t.Fatal(err)
	}
	writeSyncSettings(t, settingsPath, personal, work, claude, pi, codex, agents, false)
	recoverySourcesBefore := snapshotTree(t, personal) + snapshotTree(t, work)
	codexBefore := snapshotTree(t, codex)

	second := runEsheep(t, environment, "sync")
	assertSuccess(t, second)
	if !strings.Contains(second.stdout, "repaired") || !strings.Contains(second.stdout, "pruned") {
		t.Fatalf("second sync stdout = %s", second.stdout)
	}
	if got := snapshotTree(t, personal) + snapshotTree(t, work); got != recoverySourcesBefore {
		t.Fatal("recovery sync modified a source directory")
	}
	if got := snapshotTree(t, codex); got != codexBefore {
		t.Fatal("sync modified the disabled Codex target")
	}
	if data, err := os.ReadFile(filepath.Join(claude, "human-owned", "keep")); err != nil || string(data) != "human" {
		t.Fatalf("human-owned directory changed: %q %v", data, err)
	}
	for _, target := range []string{claude, pi, agents} {
		if _, err := os.Stat(filepath.Join(target, "beta")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stale beta remains in %q: %v", target, err)
		}
	}
	if _, err := os.Stat(filepath.Join(codex, "beta")); err != nil {
		t.Fatalf("disabled Codex beta was pruned: %v", err)
	}
	finalStatus := runEsheep(t, environment, "skills", "status", "--json")
	assertSuccess(t, finalStatus)
	assertStatusHealth(t, finalStatus.stdout, true)
}

func TestFoundationConfigurationWorkflow(t *testing.T) {
	root := filepath.Join(workDir, "foundation")
	home := filepath.Join(root, "home")
	configHome := filepath.Join(root, "config")
	personal := filepath.Join(root, "personal-skills")
	work := filepath.Join(root, "work-skills")
	for _, directory := range []string{home, configHome, personal, work} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	environment := map[string]string{
		"HOME":              home,
		"XDG_CONFIG_HOME":   configHome,
		"ESHEEP_PI_ENABLED": "false",
	}
	settingsPath := filepath.Join(configHome, "esheep", "esheep.toml")

	version := runEsheep(t, environment, "--config", filepath.Join(root, "missing.toml"), "--version")
	assertSuccess(t, version)
	if version.stdout != "esheep version "+expectedVersion+"\n" {
		t.Fatalf("version stdout = %q", version.stdout)
	}

	configuration := runEsheep(t, environment, "--claude-enabled=false", "config", "--provenance")
	assertSuccess(t, configuration)
	if !strings.Contains(configuration.stdout, "[targets.claude]\nenabled = false\n") ||
		!strings.Contains(configuration.stdout, "# targets.claude.enabled = false (source: <pflag>)") ||
		!strings.Contains(configuration.stdout, "# Resolved paths") ||
		!strings.Contains(configuration.stdout, filepath.Join(home, ".claude", "skills")) ||
		!strings.Contains(configuration.stdout, "# Provenance") {
		t.Fatalf("config output = %s", configuration.stdout)
	}
	missingConfig := runEsheep(t, environment, "--config", filepath.Join(root, "missing.toml"), "config")
	if missingConfig.exitCode != 1 || missingConfig.stdout != "" || missingConfig.stderr == "" {
		t.Fatalf("missing explicit config result = %#v", missingConfig)
	}
	if _, err := os.Stat(settingsPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("absent settings file was created: %v", err)
	}

	settings := []byte(fmt.Sprintf(`[[sources]]
name = "personal"
path = %q

[[sources]]
name = "work"
path = %q

[targets.agents]
enabled = true
`, personal, work))
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, settings, 0o640); err != nil {
		t.Fatal(err)
	}
	fixedTime := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(settingsPath, fixedTime, fixedTime); err != nil {
		t.Fatal(err)
	}
	settingsBefore, err := os.Stat(settingsPath)
	if err != nil {
		t.Fatal(err)
	}

	discovered := runEsheep(t, environment, "config", "--provenance")
	assertSuccess(t, discovered)
	var decoded struct {
		Sources []struct {
			Name string `toml:"name"`
			Path string `toml:"path"`
		} `toml:"sources"`
		Targets struct {
			Claude struct {
				Enabled bool `toml:"enabled"`
			} `toml:"claude"`
			Pi struct {
				Enabled bool `toml:"enabled"`
			} `toml:"pi"`
			Agents struct {
				Enabled bool `toml:"enabled"`
			} `toml:"agents"`
		} `toml:"targets"`
	}
	if _, err := toml.Decode(discovered.stdout, &decoded); err != nil {
		t.Fatalf("config output is not TOML: %v\n%s", err, discovered.stdout)
	}
	if len(decoded.Sources) != 2 || decoded.Sources[0].Name != "personal" || decoded.Sources[1].Path != work {
		t.Fatalf("sources = %#v", decoded.Sources)
	}
	if !decoded.Targets.Claude.Enabled || decoded.Targets.Pi.Enabled || !decoded.Targets.Agents.Enabled {
		t.Fatalf("targets = %#v", decoded.Targets)
	}
	for _, content := range []string{personal, work, "# sources.personal.path", "# sources.work.path"} {
		if !strings.Contains(discovered.stdout, content) {
			t.Fatalf("config output missing %q:\n%s", content, discovered.stdout)
		}
	}

	settingsAfter, err := os.Stat(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	settingsContents, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(settingsContents, settings) || settingsAfter.Mode() != settingsBefore.Mode() || !settingsAfter.ModTime().Equal(settingsBefore.ModTime()) {
		t.Fatalf("human-owned settings changed: before=%#v after=%#v contents=%q", settingsBefore, settingsAfter, settingsContents)
	}

	withoutHome := map[string]string{"XDG_CONFIG_HOME": configHome}
	noHome := runEsheep(t, withoutHome, "config")
	if noHome.exitCode != 1 || noHome.stdout != "" || noHome.stderr == "" {
		t.Fatalf("missing HOME result = %#v", noHome)
	}
	relativeXDG := runEsheep(t, map[string]string{"HOME": home, "XDG_CONFIG_HOME": "relative"}, "config")
	if relativeXDG.exitCode != 1 || relativeXDG.stdout != "" || relativeXDG.stderr == "" {
		t.Fatalf("relative XDG result = %#v", relativeXDG)
	}
	invalid := runEsheep(t, environment, "config", "extra")
	if invalid.exitCode != 2 || invalid.stdout != "" {
		t.Fatalf("invalid operands result = %#v", invalid)
	}
}

func writeE2ESkill(t *testing.T, source, name, description, extra string, support map[string]string) {
	t.Helper()
	root := filepath.Join(source, name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "---\nname: " + name + "\ndescription: '" + description + "'\n" + extra + "---\n# Body\n"
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	for relative, contents := range support {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o700); err != nil {
			t.Fatal(err)
		}
	}
}

func writeSyncSettings(t *testing.T, path, personal, work, claude, pi, codex, agents string, codexEnabled bool) {
	t.Helper()
	settings := fmt.Sprintf(`[[sources]]
name = "personal"
path = %q

[[sources]]
name = "work"
path = %q

[targets.claude]
enabled = true
path = %q

[targets.pi]
enabled = true
path = %q

[targets.codex]
enabled = %t
path = %q

[targets.agents]
enabled = true
path = %q
`, personal, work, claude, pi, codexEnabled, codex, agents)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(settings), 0o640); err != nil {
		t.Fatal(err)
	}
}

func assertMarker(t *testing.T, targetRoot, target, source, skill string) {
	t.Helper()
	path := filepath.Join(targetRoot, skill, ".esheep.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var marker struct {
		Skill  string `toml:"skill"`
		Source string `toml:"source"`
		Target string `toml:"target"`
	}
	metadata, err := toml.Decode(string(data), &marker)
	if err != nil {
		t.Fatalf("decode marker %q: %v", path, err)
	}
	if len(metadata.Undecoded()) != 0 || marker.Source != source || marker.Skill != skill || marker.Target != target {
		t.Fatalf("marker %q = %#v, undecoded=%v", path, marker, metadata.Undecoded())
	}
}

func assertManifestMetadata(t *testing.T, path, metadata string, present bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	actual := strings.Contains(string(data), metadata)
	if actual != present {
		t.Fatalf("manifest %q metadata %q presence = %t, want %t\n%s", path, metadata, actual, present, data)
	}
}

func assertStatusHealth(t *testing.T, output string, healthy bool) {
	t.Helper()
	var status struct {
		Healthy bool `json:"healthy"`
	}
	if err := json.Unmarshal([]byte(output), &status); err != nil {
		t.Fatalf("decode status JSON: %v\n%s", err, output)
	}
	if status.Healthy != healthy {
		t.Fatalf("status healthy = %t, want %t\n%s", status.Healthy, healthy, output)
	}
}

func snapshotTree(t *testing.T, root string) string {
	t.Helper()
	var snapshot strings.Builder
	err := filepath.WalkDir(root, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(&snapshot, "%s %s", filepath.ToSlash(relative), info.Mode())
		switch {
		case info.Mode().IsRegular():
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(&snapshot, " %q", data)
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(&snapshot, " -> %q", target)
		}
		_ = snapshot.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot.String()
}

type processResult struct {
	stdout   string
	stderr   string
	exitCode int
}

func runEsheep(t *testing.T, environment map[string]string, args ...string) processResult {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command := exec.Command(binaryPath, args...)
	command.Stdout = &stdout
	command.Stderr = &stderr
	command.Env = processEnvironment(environment)
	exitCode := 0
	if err := command.Run(); err != nil {
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) {
			t.Fatalf("run esheep: %v", err)
		}
		exitCode = exitError.ExitCode()
	}
	return processResult{stdout: stdout.String(), stderr: stderr.String(), exitCode: exitCode}
}

func assertSuccess(t *testing.T, result processResult) {
	t.Helper()
	if result.exitCode != 0 || result.stderr != "" {
		t.Fatalf("process result = %#v", result)
	}
}

func processEnvironment(overrides map[string]string) []string {
	blocked := map[string]struct{}{
		"HOME": {}, "XDG_CONFIG_HOME": {},
	}
	var environment []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, found := blocked[key]; found || strings.HasPrefix(key, "ESHEEP_") {
			continue
		}
		if _, found := overrides[key]; !found {
			environment = append(environment, entry)
		}
	}
	for key, value := range overrides {
		environment = append(environment, key+"="+value)
	}
	return environment
}
