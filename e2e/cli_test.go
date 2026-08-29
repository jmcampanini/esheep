package e2e

import (
	"bytes"
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

	configuration := runEsheep(t, environment, "--claude-enabled=true", "config", "--provenance")
	assertSuccess(t, configuration)
	if !strings.Contains(configuration.stdout, "# Resolved paths") || !strings.Contains(configuration.stdout, filepath.Join(home, ".claude", "skills")) || !strings.Contains(configuration.stdout, "# Provenance") {
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
