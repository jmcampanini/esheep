package config

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

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
	if got.Memory.Path != "~/.local/share/esheep/memory" {
		t.Fatalf("default memory path = %q", got.Memory.Path)
	}
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
	absoluteHome := filepath.Join(t.TempDir(), "home")
	tests := []struct {
		name string
		env  map[string]string
	}{
		{name: "missing home", env: map[string]string{}},
		{name: "relative home", env: map[string]string{"HOME": "home"}},
		{name: "relative data home", env: map[string]string{"HOME": absoluteHome, "XDG_DATA_HOME": "data"}},
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
	if err := os.WriteFile(filepath.Join(cwd, "config.toml"), []byte("[memory]\npath = \"~/discovered\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := testEnv(filepath.Join(t.TempDir(), "home"), filepath.Join(t.TempDir(), "config"))
	result, err := Load(LoadOptions{Env: env})
	if err != nil {
		t.Fatal(err)
	}
	if result.Config.Memory.Path != defaults().Memory.Path || len(result.Report.LoadedFiles) != 0 {
		t.Fatalf("cwd config affected result = %#v report = %#v", result.Config.Memory, result.Report)
	}
}

func TestExplicitConfigIsRequiredAndReplacesDiscovery(t *testing.T) {
	home := t.TempDir()
	configHome := filepath.Join(t.TempDir(), "xdg")
	if err := os.MkdirAll(filepath.Join(configHome, "esheep"), 0o755); err != nil {
		t.Fatal(err)
	}
	discovered := filepath.Join(configHome, "esheep", "config.toml")
	if err := os.WriteFile(discovered, []byte("[memory]\npath = \"~/discovered\"\n"), 0o600); err != nil {
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
	if err := os.WriteFile(explicit, []byte("[memory]\npath = \"~/explicit\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Load(LoadOptions{Env: env, ConfigPath: explicit})
	if err != nil {
		t.Fatal(err)
	}
	if result.Config.Memory.Path != "~/explicit" || result.Locations.ConfigFile != explicit {
		t.Fatalf("explicit result = %#v", result)
	}
}

func TestMemoryPathMustNotBeTooBroad(t *testing.T) {
	home, err := canonicalPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{home, "~", string(filepath.Separator)} {
		if _, err := resolveMemory(path, home); err == nil || !strings.Contains(err.Error(), "too broad") {
			t.Errorf("resolveMemory(%q) error = %v, want too broad", path, err)
		}
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

	got, err := canonicalPath(filepath.Join(alias, "missing", "memory"))

	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(realParent, "missing", "memory"); got != want {
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
	result.Config.Memory.Path = "sensitive-value"
	result.ResolvedMemory = "/sensitive-value"
	result.Report.Updates["memory.path"] = "source\r\n# injected"
	output, err := Render(result, ReportOptions{Provenance: true, Redact: func(result LoadResult) LoadResult {
		result.Config.Memory.Path = "REDACTED"
		result.ResolvedMemory = "REDACTED"
		return result
	}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(output), "sensitive-value") || !strings.Contains(string(output), "# memory.path = \"REDACTED\"") || !strings.Contains(string(output), "# Resolved paths") || strings.Contains(string(output), "source\r\n") {
		t.Fatalf("report = %s", output)
	}
	var decoded Config
	if _, err := toml.Decode(string(output), &decoded); err != nil {
		t.Fatalf("rendered output is not TOML: %v\n%s", err, output)
	}
}

func TestMachinesLoadFromTOMLWithDefaults(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	configHome := filepath.Join(t.TempDir(), "config")
	if err := os.MkdirAll(filepath.Join(configHome, "esheep"), 0o755); err != nil {
		t.Fatal(err)
	}
	settings := "[[machines]]\nname = \"nas\"\n\n[[machines]]\nname = \"studio\"\nhost = \"javier@studio.example\"\ncommand = \"/opt/homebrew/bin/esheep\"\ntimeout = \"2m\"\n"
	if err := os.WriteFile(filepath.Join(configHome, "esheep", "config.toml"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := Load(LoadOptions{Env: testEnv(home, configHome)})

	if err != nil {
		t.Fatal(err)
	}
	want := []ResolvedMachine{
		{Command: "esheep", Host: "nas", Name: "nas", Timeout: time.Minute},
		{Command: "/opt/homebrew/bin/esheep", Host: "javier@studio.example", Name: "studio", Timeout: 2 * time.Minute},
	}
	if !reflect.DeepEqual(result.ResolvedMachines, want) {
		t.Fatalf("resolved machines = %#v, want %#v", result.ResolvedMachines, want)
	}
	output, err := Render(result, ReportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		"[[machines]]",
		`# machines.nas.host = "nas"`,
		`# machines.nas.command = "esheep"`,
		`# machines.nas.timeout = "1m0s"`,
		`# machines.studio.host = "javier@studio.example"`,
		`# machines.studio.timeout = "2m0s"`,
	} {
		if !strings.Contains(string(output), line) {
			t.Errorf("rendered config missing %q:\n%s", line, output)
		}
	}
}

func TestMachinesRejectInvalidEntries(t *testing.T) {
	tests := []struct {
		machines []Machine
		name     string
		want     string
	}{
		{name: "empty name", machines: []Machine{{}}, want: "must be a non-empty hostname"},
		{name: "comma in name", machines: []Machine{{Name: "a,b"}}, want: "must be a non-empty hostname"},
		{name: "reserved all", machines: []Machine{{Name: "ALL"}}, want: "reserved"},
		{name: "duplicate ignoring case", machines: []Machine{{Name: "nas"}, {Name: "NAS"}}, want: "duplicate machine name"},
		{name: "bad timeout", machines: []Machine{{Name: "nas", Timeout: "soon"}}, want: "timeout"},
		{name: "zero timeout", machines: []Machine{{Name: "nas", Timeout: "0s"}}, want: "must be positive"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := resolveMachines(test.machines)

			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("resolveMachines() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestMemoryPathDefaultsAndOverrides(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	configHome := filepath.Join(t.TempDir(), "config")
	dataHome := filepath.Join(t.TempDir(), "data")
	tests := []struct {
		env       map[string]string
		flags     []string
		name      string
		want      string
		wantValue string
	}{
		{name: "default under home", env: testEnv(home, configHome), want: filepath.Join(home, ".local", "share", "esheep", "memory"), wantValue: "<default>"},
		{name: "XDG_DATA_HOME default", env: withEnv(testEnv(home, configHome), "XDG_DATA_HOME", dataHome), want: filepath.Join(dataHome, "esheep", "memory"), wantValue: "XDG_DATA_HOME"},
		{name: "ESHEEP_MEMORY_PATH wins over XDG_DATA_HOME", env: withEnv(withEnv(testEnv(home, configHome), "XDG_DATA_HOME", dataHome), "ESHEEP_MEMORY_PATH", "~/memories"), want: filepath.Join(home, "memories"), wantValue: "<env>"},
		{name: "flag wins over environment", env: withEnv(testEnv(home, configHome), "ESHEEP_MEMORY_PATH", "~/memories"), flags: []string{"--memory-path=~/from-flag"}, want: filepath.Join(home, "from-flag"), wantValue: "<pflag>"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			flags.SetOutput(io.Discard)
			if err := RegisterFlags(flags); err != nil {
				t.Fatal(err)
			}
			if err := flags.Parse(test.flags); err != nil {
				t.Fatal(err)
			}

			result, err := Load(LoadOptions{Env: test.env, Flags: flags})
			if err != nil {
				t.Fatalf("Load: %v", err)
			}

			want, err := canonicalPath(test.want)
			if err != nil {
				t.Fatal(err)
			}
			if result.ResolvedMemory != want {
				t.Fatalf("ResolvedMemory = %q, want %q", result.ResolvedMemory, want)
			}
			if got := result.Report.Updates["memory.path"]; got != test.wantValue {
				t.Fatalf("provenance = %q, want %q", got, test.wantValue)
			}
		})
	}
}

func TestMemoryPathMustBeAbsoluteAndDataHomeMustBeAbsolute(t *testing.T) {
	env := testEnv(filepath.Join(t.TempDir(), "home"), filepath.Join(t.TempDir(), "config"))

	env["ESHEEP_MEMORY_PATH"] = "relative/memory"
	if _, err := Load(LoadOptions{Env: env}); err == nil || !strings.Contains(err.Error(), "memory.path") {
		t.Fatalf("relative memory path error = %v", err)
	}
	delete(env, "ESHEEP_MEMORY_PATH")
	env["XDG_DATA_HOME"] = "data"
	if _, err := Load(LoadOptions{Env: env}); err == nil || !strings.Contains(err.Error(), "XDG_DATA_HOME") {
		t.Fatalf("relative XDG_DATA_HOME error = %v", err)
	}
}

func TestRenderReportsResolvedMemoryPath(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	result, err := Load(LoadOptions{Env: testEnv(home, filepath.Join(t.TempDir(), "config"))})
	if err != nil {
		t.Fatal(err)
	}

	output, err := Render(result, ReportOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(output), "# memory.path = "+strconv.Quote(result.ResolvedMemory)) {
		t.Fatalf("report = %s", output)
	}
}

func withEnv(env map[string]string, key, value string) map[string]string {
	copied := make(map[string]string, len(env)+1)
	for k, v := range env {
		copied[k] = v
	}
	copied[key] = value
	return copied
}
