package config

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/spf13/pflag"
)

func testEnv(home, config, state, data string) map[string]string {
	return map[string]string{
		"HOME":                  home,
		"XDG_CONFIG_HOME":       config,
		"XDG_STATE_HOME":        state,
		"XDG_DATA_HOME":         data,
		"ESHEEP_CLAUDE_ENABLED": "true",
	}
}

func TestDefaultsAndLocationsUseXDGAndFourTargetDefaults(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	env := testEnv(home, filepath.Join(t.TempDir(), "config"), filepath.Join(t.TempDir(), "state"), filepath.Join(t.TempDir(), "data"))
	got := defaults()
	if !got.Targets.Claude.Enabled || !got.Targets.Pi.Enabled || !got.Targets.Codex.Enabled || got.Targets.Agents.Enabled {
		t.Fatalf("default enabled targets = %#v", got.Targets)
	}
	if got.Targets.Claude.Path != "~/.claude/skills" || got.Targets.Pi.Path != "~/.pi/agent/skills" || got.Targets.Codex.Path != "~/.codex/skills" || got.Targets.Agents.Path != "~/.agents/skills" {
		t.Fatalf("default target paths = %#v", got.Targets)
	}
	locations := locationsFromEnv(env)
	if locations.ConfigFile != filepath.Join(env["XDG_CONFIG_HOME"], "esheep", "esheep.toml") || locations.Registry != filepath.Join(env["XDG_STATE_HOME"], "esheep", "repos.toml") || locations.CloneRoot != filepath.Join(env["XDG_DATA_HOME"], "esheep", "repos") {
		t.Fatalf("locations = %#v", locations)
	}
}

func TestRelativeXDGHomeIsResolvedFromCurrentDirectory(t *testing.T) {
	cwd := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	actualCWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"HOME": filepath.Join(cwd, "home"), "XDG_CONFIG_HOME": "relative-config"}
	got := locationsFromEnv(env)
	if got.ConfigFile != filepath.Join(actualCWD, "relative-config", "esheep", "esheep.toml") {
		t.Fatalf("ConfigFile = %q", got.ConfigFile)
	}
}

func TestLoadDoesNotDiscoverCwdConfig(t *testing.T) {
	cwd := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	if err := os.WriteFile(filepath.Join(cwd, "esheep.toml"), []byte("[targets.claude]\nenabled = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := testEnv(filepath.Join(t.TempDir(), "home"), filepath.Join(t.TempDir(), "config"), filepath.Join(t.TempDir(), "state"), filepath.Join(t.TempDir(), "data"))
	result, err := Load(LoadOptions{Env: env})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Config.Targets.Claude.Enabled || len(result.Report.LoadedFiles) != 0 {
		t.Fatalf("cwd config affected result = %#v report = %#v", result.Config.Targets, result.Report)
	}
}

func TestApprovedEnvironmentNamesLoadEachTarget(t *testing.T) {
	env := testEnv(filepath.Join(t.TempDir(), "home"), filepath.Join(t.TempDir(), "config"), filepath.Join(t.TempDir(), "state"), filepath.Join(t.TempDir(), "data"))
	env["ESHEEP_PI_ENABLED"] = "false"
	env["ESHEEP_CODEX_PATH"] = "~/codex-overridden"
	env["ESHEEP_AGENTS_PATH"] = "~/agents-overridden"
	result, err := Load(LoadOptions{Env: env})
	if err != nil {
		t.Fatal(err)
	}
	if result.Config.Targets.Pi.Enabled || result.Config.Targets.Codex.Path != "~/codex-overridden" || result.Config.Targets.Agents.Path != "~/agents-overridden" {
		t.Fatalf("environment values = %#v", result.Config.Targets)
	}
	if result.Report.Updates["targets.pi.enabled"] != "<env>" || result.Report.Updates["targets.codex.path"] != "<env>" || result.Report.Updates["targets.agents.path"] != "<env>" {
		t.Fatalf("environment provenance = %#v", result.Report.Updates)
	}
}

func TestLoadPrecedenceAndProvenance(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	configHome := filepath.Join(t.TempDir(), "xdg")
	state, data := filepath.Join(t.TempDir(), "state"), filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(filepath.Join(configHome, "esheep"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(configHome, "esheep", "esheep.toml")
	if err := os.WriteFile(path, []byte("[targets.claude]\nenabled = false\npath = \"~/from-file\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := testEnv(home, configHome, state, data)
	env["ESHEEP_CLAUDE_ENABLED"] = "true"
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.SetOutput(io.Discard)
	if err := RegisterFlags(flags); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"claude-enabled", "claude-path", "pi-enabled", "pi-path", "codex-enabled", "codex-path", "agents-enabled", "agents-path"} {
		if flags.Lookup(name) == nil {
			t.Fatalf("missing approved flag --%s", name)
		}
	}
	if err := flags.Parse([]string{"--claude-enabled=false", "--claude-path=~/from-flag"}); err != nil {
		t.Fatal(err)
	}
	result, err := Load(LoadOptions{Env: env, Flags: flags})
	if err != nil {
		t.Fatal(err)
	}
	if result.Config.Targets.Claude.Enabled || result.Config.Targets.Claude.Path != "~/from-flag" || result.ResolvedTargets.Claude != filepath.Join(home, "from-flag") {
		t.Fatalf("effective Claude target = %#v resolved = %#v", result.Config.Targets.Claude, result.ResolvedTargets)
	}
	if result.Report.Updates["targets.claude.enabled"] != "<pflag>" || result.Report.Updates["targets.claude.path"] != "<pflag>" {
		t.Fatalf("provenance = %#v", result.Report.Updates)
	}
	if result.Report.Updates["targets.pi.enabled"] != "<default>" {
		t.Fatalf("default provenance = %#v", result.Report.Updates)
	}
}

func TestExplicitConfigIsRequiredAndReplacesDiscovery(t *testing.T) {
	home := t.TempDir()
	configHome := filepath.Join(t.TempDir(), "xdg")
	if err := os.MkdirAll(filepath.Join(configHome, "esheep"), 0o755); err != nil {
		t.Fatal(err)
	}
	discovered := filepath.Join(configHome, "esheep", "esheep.toml")
	if err := os.WriteFile(discovered, []byte("[targets.claude]\nenabled = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := testEnv(home, configHome, filepath.Join(t.TempDir(), "s"), filepath.Join(t.TempDir(), "d"))
	if _, err := Load(LoadOptions{Env: env, ConfigPath: filepath.Join(t.TempDir(), "missing.toml")}); err == nil || !strings.Contains(err.Error(), "required config file") {
		t.Fatalf("missing explicit config error = %v", err)
	}
	emptyFlags := pflag.NewFlagSet("empty-config", pflag.ContinueOnError)
	emptyFlags.SetOutput(io.Discard)
	if err := RegisterFlags(emptyFlags); err != nil {
		t.Fatal(err)
	}
	if err := emptyFlags.Parse([]string{"--config="}); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(LoadOptions{Env: env, Flags: emptyFlags}); err == nil || !strings.Contains(err.Error(), "file path is empty") {
		t.Fatalf("empty explicit config error = %v", err)
	}
	explicit := filepath.Join(t.TempDir(), "explicit.toml")
	if err := os.WriteFile(explicit, []byte("[targets.pi]\nenabled = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Load(LoadOptions{Env: env, ConfigPath: explicit})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Config.Targets.Claude.Enabled || result.Config.Targets.Pi.Enabled || result.Locations.ConfigFile != filepath.Clean(explicit) {
		t.Fatalf("explicit effective config = %#v locations = %#v", result.Config.Targets, result.Locations)
	}
}

func TestUnknownTOMLKeyFails(t *testing.T) {
	env := testEnv(t.TempDir(), filepath.Join(t.TempDir(), "config"), filepath.Join(t.TempDir(), "state"), filepath.Join(t.TempDir(), "data"))
	path := filepath.Join(t.TempDir(), "unknown.toml")
	if err := os.WriteFile(path, []byte("unknown = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(LoadOptions{Env: env, ConfigPath: path}); err == nil || !strings.Contains(err.Error(), "unknown keys") {
		t.Fatalf("unknown-key error = %v", err)
	}
}

func TestRenderedConfigRoundTripsThroughExplicitLoading(t *testing.T) {
	env := testEnv(filepath.Join(t.TempDir(), "home"), filepath.Join(t.TempDir(), "config"), filepath.Join(t.TempDir(), "state"), filepath.Join(t.TempDir(), "data"))
	result, err := Load(LoadOptions{Env: env})
	if err != nil {
		t.Fatal(err)
	}
	output, err := Render(result, ReportOptions{Provenance: true})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "redirected.toml")
	if err := os.WriteFile(path, output, 0o600); err != nil {
		t.Fatal(err)
	}
	roundTripped, err := Load(LoadOptions{Env: env, ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if roundTripped.Config != result.Config {
		t.Fatalf("round-tripped config = %#v, want %#v", roundTripped.Config, result.Config)
	}
}

func TestRenderIsValidTOMLIncludesDerivedProvenanceAndRedactsBeforeFormatting(t *testing.T) {
	env := testEnv(filepath.Join(t.TempDir(), "home"), filepath.Join(t.TempDir(), "config"), filepath.Join(t.TempDir(), "state"), filepath.Join(t.TempDir(), "data"))
	result, err := Load(LoadOptions{Env: env})
	if err != nil {
		t.Fatal(err)
	}
	if result.Config.Targets.Pi.Path != "~/.pi/agent/skills" {
		t.Fatalf("effective default spelling = %q", result.Config.Targets.Pi.Path)
	}
	result.Config.Targets.Claude.Path = "sensitive-value"
	result.ResolvedTargets.Claude = "/sensitive-value"
	result.Report.Updates["targets.claude.path"] = "source\r\n# injected"
	output, err := Render(result, ReportOptions{Provenance: true, Redact: func(result LoadResult) LoadResult {
		result.Config.Targets.Claude.Path = "REDACTED"
		result.ResolvedTargets.Claude = "REDACTED"
		return result
	}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(output), "sensitive-value") || !strings.Contains(string(output), "# targets.claude.path = \"REDACTED\"") || !strings.Contains(string(output), "# Derived values") || !strings.Contains(string(output), "# Provenance") || strings.Contains(string(output), "source\r\n") {
		t.Fatalf("report = %s", output)
	}
	if !strings.Contains(string(output), "path = \"~/.pi/agent/skills\"") {
		t.Fatalf("raw or derived target path missing from report = %s", output)
	}
	var decoded Config
	if _, err := toml.Decode(string(output), &decoded); err != nil {
		t.Fatalf("rendered output is not TOML: %v\n%s", err, output)
	}
}
