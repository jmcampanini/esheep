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
	"github.com/jmcampanini/esheep/internal/registry"
)

var (
	binaryPath      string
	expectedVersion string
	workDir         string
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
	defer func() {
		_ = os.RemoveAll(workDir)
	}()

	expectedVersion = gitVersion(repositoryRoot)
	build := exec.Command("make", "BUILD_DIR="+workDir, "build")
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

func TestFoundationRepositoryWorkflow(t *testing.T) {
	root, err := os.MkdirTemp(workDir, "foundation-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(root)
	})
	home := filepath.Join(root, "home")
	configHome := filepath.Join(root, "config")
	stateHome := filepath.Join(root, "state")
	dataHome := filepath.Join(root, "data")
	for _, directory := range []string{home, configHome, stateHome, dataHome} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	repoA := createGitRepository(t, root, "repo-a")
	repoB := createGitRepository(t, root, "repo-b")
	guardBin := filepath.Join(root, "guard-bin")
	if err := os.Mkdir(guardBin, 0o755); err != nil {
		t.Fatal(err)
	}
	gitUsed := filepath.Join(root, "git-used")
	guard := "#!/bin/sh\nprintf invoked >> " + shellQuote(gitUsed) + "\nexit 99\n"
	if err := os.WriteFile(filepath.Join(guardBin, "git"), []byte(guard), 0o755); err != nil {
		t.Fatal(err)
	}
	environment := map[string]string{
		"HOME":              home,
		"XDG_CONFIG_HOME":   configHome,
		"XDG_STATE_HOME":    stateHome,
		"XDG_DATA_HOME":     dataHome,
		"PATH":              guardBin + string(os.PathListSeparator) + os.Getenv("PATH"),
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
	var decoded struct {
		Targets struct {
			Claude struct {
				Enabled bool   `toml:"enabled"`
				Path    string `toml:"path"`
			} `toml:"claude"`
			Pi struct {
				Enabled bool `toml:"enabled"`
			} `toml:"pi"`
		} `toml:"targets"`
	}
	if _, err := toml.Decode(configuration.stdout, &decoded); err != nil {
		t.Fatalf("config output is not TOML: %v\n%s", err, configuration.stdout)
	}
	if !decoded.Targets.Claude.Enabled || decoded.Targets.Pi.Enabled {
		t.Fatalf("effective targets = %#v", decoded.Targets)
	}
	if decoded.Targets.Claude.Path != "~/.claude/skills" {
		t.Fatalf("Claude path = %q", decoded.Targets.Claude.Path)
	}
	for _, content := range []string{"# Derived values", filepath.Join(stateHome, "esheep", "repos.toml"), filepath.Join(home, ".claude", "skills"), "# Provenance"} {
		if !strings.Contains(configuration.stdout, content) {
			t.Fatalf("config output missing %q:\n%s", content, configuration.stdout)
		}
	}
	missingConfig := runEsheep(t, environment, "--config", filepath.Join(root, "missing.toml"), "config")
	if missingConfig.exitCode != 1 || missingConfig.stdout != "" || !strings.Contains(missingConfig.stderr, "required config file") {
		t.Fatalf("missing explicit config result = %#v", missingConfig)
	}
	if _, err := os.Stat(settingsPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("absent settings file was created: %v", err)
	}

	settings := []byte("[targets.agents]\nenabled = true\n")
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
	discoveredConfig := runEsheep(t, environment, "config")
	assertSuccess(t, discoveredConfig)
	if !strings.Contains(discoveredConfig.stdout, "[targets.agents]\nenabled = true") {
		t.Fatalf("discovered config output = %s", discoveredConfig.stdout)
	}

	addA := runEsheep(t, environment, "repo", "add", repoA)
	assertSuccess(t, addA)
	if addA.stdout != "Added repo-a\n" {
		t.Fatalf("repo add stdout = %q", addA.stdout)
	}
	addB := runEsheep(t, environment, "repo", "add", repoB, "--name", "work/repo-b")
	assertSuccess(t, addB)
	remoteURL := "git@example.invalid:org/repo.git"
	addRemote := runEsheep(t, environment, "repo", "add", remoteURL)
	assertSuccess(t, addRemote)

	listed := runEsheep(t, environment, "repo", "list")
	assertSuccess(t, listed)
	for _, content := range []string{"NAME", "URL", "repo-a", repoA, "work/repo-b", repoB, "example.invalid/org/repo", remoteURL} {
		if !strings.Contains(listed.stdout, content) {
			t.Fatalf("repo list missing %q:\n%s", content, listed.stdout)
		}
	}

	registryPath := filepath.Join(stateHome, "esheep", "repos.toml")
	registered, err := registry.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(registered.Repos) != 3 || registered.Repos[0].Name != "repo-a" || registered.Repos[1].Name != "work/repo-b" || registered.Repos[2].Name != "example.invalid/org/repo" {
		t.Fatalf("registry = %#v", registered)
	}
	beforeInvalid, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	invalid := runEsheep(t, environment, "repo", "add", repoA, "extra")
	if invalid.exitCode != 2 || invalid.stdout != "" {
		t.Fatalf("invalid result = %#v", invalid)
	}
	afterInvalid, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeInvalid, afterInvalid) {
		t.Fatal("invalid operands changed the registry")
	}

	clone := filepath.Join(dataHome, "esheep", "repos", "work-repo-b")
	if err := os.MkdirAll(clone, 0o755); err != nil {
		t.Fatal(err)
	}
	removed := runEsheep(t, environment, "repo", "remove", "work/repo-b")
	assertSuccess(t, removed)
	if _, err := os.Stat(clone); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed clone stat error = %v", err)
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
	if _, err := os.Stat(gitUsed); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("repo registration invoked git: %v", err)
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
		"HOME": {}, "XDG_CONFIG_HOME": {}, "XDG_STATE_HOME": {}, "XDG_DATA_HOME": {},
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

func createGitRepository(t *testing.T, parent, name string) string {
	t.Helper()
	path := filepath.Join(parent, name)
	git := exec.Command("git", "init", "-q", path)
	if output, err := git.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	return path
}

func gitVersion(repositoryRoot string) string {
	command := exec.Command("git", "describe", "--tags", "--dirty", "--always")
	command.Dir = repositoryRoot
	output, err := command.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(output))
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
