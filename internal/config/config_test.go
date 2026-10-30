package config

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/spf13/pflag"
)

func testEnv(home, configHome string) map[string]string {
	return map[string]string{
		"HOME":            home,
		"XDG_CONFIG_HOME": configHome,
	}
}

func TestDefaultsAndLocation(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	configHome := filepath.Join(t.TempDir(), "config")
	got := defaults()
	if got.Sessions.Claude.Path != "~/.claude/projects" || got.Sessions.Pi.Path != "~/.pi/agent/sessions" || got.Sessions.Codex.Home != "~/.codex" {
		t.Fatalf("default session paths = %#v", got.Sessions)
	}
	location := locationsFromEnv(testEnv(home, configHome), home)
	if location.ConfigFile != filepath.Join(configHome, "esheep", "config.toml") {
		t.Fatalf("location = %#v", location)
	}
}

func TestDiscoveredConfigFallsBackToHomeConfigDirectory(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	home, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		configHome string
	}{
		{name: "unset"},
		{name: "relative", configHome: "config"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := testEnv(home, test.configHome)
			if test.configHome == "" {
				delete(env, "XDG_CONFIG_HOME")
			}

			result, err := Load(LoadOptions{Env: env})
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}

			want := filepath.Join(home, ".config", "esheep", "config.toml")
			if result.Locations.ConfigFile != want {
				t.Errorf("Load().Locations.ConfigFile = %q, want %q", result.Locations.ConfigFile, want)
			}
		})
	}
}

func TestEnvironmentBoundariesFailLoudly(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{name: "missing home", env: map[string]string{}},
		{name: "relative home", env: map[string]string{"HOME": "home"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Load(LoadOptions{Env: test.env}); err == nil {
				t.Fatal("Load succeeded")
			}
		})
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
	if err := os.WriteFile(filepath.Join(cwd, "config.toml"), []byte("[sessions.claude]\npath = \"~/discovered\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := testEnv(filepath.Join(t.TempDir(), "home"), filepath.Join(t.TempDir(), "config"))
	result, err := Load(LoadOptions{Env: env})
	if err != nil {
		t.Fatal(err)
	}
	if result.Config.Sessions.Claude.Path != defaults().Sessions.Claude.Path || len(result.Report.LoadedFiles) != 0 {
		t.Fatalf("cwd config affected result = %#v report = %#v", result.Config.Sessions, result.Report)
	}
}

func TestExplicitConfigIsRequiredAndReplacesDiscovery(t *testing.T) {
	home := t.TempDir()
	configHome := filepath.Join(t.TempDir(), "xdg")
	if err := os.MkdirAll(filepath.Join(configHome, "esheep"), 0o755); err != nil {
		t.Fatal(err)
	}
	discovered := filepath.Join(configHome, "esheep", "config.toml")
	if err := os.WriteFile(discovered, []byte("[sessions.claude]\npath = \"~/discovered\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := testEnv(home, configHome)
	if _, err := Load(LoadOptions{Env: env, ConfigPath: filepath.Join(t.TempDir(), "missing.toml")}); err == nil {
		t.Fatal("missing explicit config succeeded")
	}
	emptyFlags := pflag.NewFlagSet("empty-config", pflag.ContinueOnError)
	emptyFlags.SetOutput(io.Discard)
	if err := RegisterFlags(emptyFlags); err != nil {
		t.Fatal(err)
	}
	if err := emptyFlags.Parse([]string{"--config="}); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(LoadOptions{Env: env, Flags: emptyFlags}); err == nil {
		t.Fatal("empty explicit config succeeded")
	}
	explicit := filepath.Join(t.TempDir(), "explicit.toml")
	if err := os.WriteFile(explicit, []byte("[sessions.claude]\npath = \"~/explicit\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Load(LoadOptions{Env: env, ConfigPath: explicit})
	if err != nil {
		t.Fatal(err)
	}
	if result.Config.Sessions.Claude.Path != "~/explicit" || result.Locations.ConfigFile != explicit {
		t.Fatalf("explicit result = %#v", result)
	}
}

func TestMissingPathResolvesExistingSymlinkedParent(t *testing.T) {
	root, err := canonicalPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	realParent := filepath.Join(root, "real")
	if err := os.Mkdir(realParent, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(realParent, alias); err != nil {
		t.Fatal(err)
	}

	got, err := canonicalPath(filepath.Join(alias, "missing", "sessions"))

	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(realParent, "missing", "sessions"); got != want {
		t.Fatalf("canonicalPath() = %q, want %q", got, want)
	}
}

func TestUnknownTOMLKeyFails(t *testing.T) {
	env := testEnv(t.TempDir(), filepath.Join(t.TempDir(), "config"))
	path := filepath.Join(t.TempDir(), "unknown.toml")
	if err := os.WriteFile(path, []byte("unknown = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(LoadOptions{Env: env, ConfigPath: path}); err == nil {
		t.Fatal("unknown key succeeded")
	}
}

func TestRenderedConfigRoundTripsThroughExplicitLoading(t *testing.T) {
	env := testEnv(filepath.Join(t.TempDir(), "home"), filepath.Join(t.TempDir(), "config"))
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
	if !reflect.DeepEqual(roundTripped.Config, result.Config) {
		t.Fatalf("round-tripped config = %#v, want %#v", roundTripped.Config, result.Config)
	}
}

func TestRenderIsValidTOMLAndSanitizesProvenance(t *testing.T) {
	env := testEnv(filepath.Join(t.TempDir(), "home"), filepath.Join(t.TempDir(), "config"))
	result, err := Load(LoadOptions{Env: env})
	if err != nil {
		t.Fatal(err)
	}
	result.Config.Sessions.Claude.Path = "sensitive-value"
	result.ResolvedSessions.Claude = "/sensitive-value"
	result.Report.Updates["sessions.claude.path"] = "source\r\n# injected"
	output, err := Render(result, ReportOptions{Provenance: true, Redact: func(result LoadResult) LoadResult {
		result.Config.Sessions.Claude.Path = "REDACTED"
		result.ResolvedSessions.Claude = "REDACTED"
		return result
	}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(output), "sensitive-value") || !strings.Contains(string(output), "# sessions.claude.path = \"REDACTED\"") || !strings.Contains(string(output), "# Resolved paths") || strings.Contains(string(output), "source\r\n") {
		t.Fatalf("report = %s", output)
	}
	var decoded Config
	if _, err := toml.Decode(string(output), &decoded); err != nil {
		t.Fatalf("rendered output is not TOML: %v\n%s", err, output)
	}
}
