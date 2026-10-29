package config

import (
	"fmt"
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

func TestMemoryPathRejectsEquivalentHomeSpellings(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, test := range []struct {
			name   string
			home   string
			memory string
		}{
			{name: "case", home: "Home", memory: "HOME"},
			{name: "composed home", home: "Jos\u00e9", memory: "Jose\u0301"},
			{name: "decomposed home", home: "Jose\u0301", memory: "Jos\u00e9"},
			{name: "case and normalization", home: "Jos\u00e9", memory: "JOSE\u0301"},
			{name: "Greek iota case and accents", home: "\u0390", memory: "\u0399\u0308\u0301"},
			{name: "Greek upsilon case and accents", home: "\u03b0", memory: "\u03a5\u0308\u0301"},
			{name: "combining mark order", home: "a\u0323\u0302", memory: "a\u0302\u0323"},
		} {
			t.Run(fmt.Sprintf("%s/existing=%t", test.name, existing), func(t *testing.T) {
				base := t.TempDir()
				home := filepath.Join(base, test.home)
				memory := filepath.Join(base, test.memory)
				if existing {
					if err := os.Mkdir(home, 0o700); err != nil {
						t.Fatal(err)
					}
				}
				env := testEnv(home, filepath.Join(base, "config"))
				env["ESHEEP_MEMORY_PATH"] = memory

				if _, err := Load(LoadOptions{Env: env}); err == nil || !strings.Contains(err.Error(), "memory.path is too broad") {
					t.Errorf("Load() with HOME=%q, memory.path=%q error = %v, want too broad", home, memory, err)
				}
			})
		}
	}
}

func TestMemoryPathResolvesHomeSymlinkBeforeComparison(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "Jos\u00e9")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(home, alias); err != nil {
		t.Fatal(err)
	}
	for _, paths := range [][2]string{{home, alias}, {alias, home}} {
		env := testEnv(paths[0], filepath.Join(base, "config"))
		env["ESHEEP_MEMORY_PATH"] = paths[1]
		if _, err := Load(LoadOptions{Env: env}); err == nil || !strings.Contains(err.Error(), "memory.path is too broad") {
			t.Errorf("Load() with HOME=%q, memory.path=%q error = %v, want too broad", paths[0], paths[1], err)
		}
	}
}

func TestMemoryPathPreservesDistinctUnicodePaths(t *testing.T) {
	base, err := canonicalPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(base, "Jos\u00e9")
	for _, path := range []string{
		filepath.Join(home, "me\u0301mories"),
		filepath.Join(base, "Jose"),
		filepath.Join(base, "Jos\u00e9-memories"),
	} {
		env := testEnv(home, filepath.Join(base, "config"))
		env["ESHEEP_MEMORY_PATH"] = path
		result, err := Load(LoadOptions{Env: env})
		if err != nil {
			t.Fatal(err)
		}
		if result.ResolvedMemory != path {
			t.Errorf("resolved memory = %q, want original spelling %q", result.ResolvedMemory, path)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("Load created memory directory %q: %v", path, err)
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
		tomlPath  string
		want      string
		wantValue string
	}{
		{name: "default under home", env: testEnv(home, configHome), want: filepath.Join(home, ".local", "share", "esheep", "memory"), wantValue: "<default>"},
		{name: "TOML", env: testEnv(home, configHome), tomlPath: "~/configured", want: filepath.Join(home, "configured"), wantValue: "file"},
		{name: "empty XDG_DATA_HOME preserves TOML", env: withEnv(testEnv(home, configHome), "XDG_DATA_HOME", ""), tomlPath: "~/configured", want: filepath.Join(home, "configured"), wantValue: "file"},
		{name: "XDG_DATA_HOME overrides TOML", env: withEnv(testEnv(home, configHome), "XDG_DATA_HOME", dataHome), tomlPath: "~/configured", want: filepath.Join(dataHome, "esheep", "memory"), wantValue: "XDG_DATA_HOME"},
		{name: "ESHEEP_MEMORY_PATH wins over XDG_DATA_HOME", env: withEnv(withEnv(testEnv(home, configHome), "XDG_DATA_HOME", dataHome), "ESHEEP_MEMORY_PATH", "~/memories"), tomlPath: "~/configured", want: filepath.Join(home, "memories"), wantValue: "<env>"},
		{name: "ESHEEP_MEMORY_PATH equal to XDG_DATA_HOME is used directly", env: withEnv(withEnv(testEnv(home, configHome), "XDG_DATA_HOME", dataHome), "ESHEEP_MEMORY_PATH", dataHome), want: dataHome, wantValue: "<env>"},
		{name: "flag wins over environment", env: withEnv(withEnv(testEnv(home, configHome), "XDG_DATA_HOME", dataHome), "ESHEEP_MEMORY_PATH", "~/memories"), tomlPath: "~/configured", flags: []string{"--memory-path=~/from-flag"}, want: filepath.Join(home, "from-flag"), wantValue: "<pflag>"},
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

			options := LoadOptions{Env: test.env, Flags: flags}
			if test.tomlPath != "" {
				options.ConfigPath = filepath.Join(t.TempDir(), "settings.toml")
				if err := os.WriteFile(options.ConfigPath, fmt.Appendf(nil, "[memory]\npath = %q\n", test.tomlPath), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			result, err := Load(options)
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
			wantSource := test.wantValue
			if wantSource == "file" {
				wantSource = options.ConfigPath
			}
			if got := result.Report.Updates["memory.path"]; got != wantSource {
				t.Fatalf("provenance = %q, want %q", got, wantSource)
			}
			rendered, err := Render(result, ReportOptions{Provenance: true})
			if err != nil {
				t.Fatal(err)
			}
			provenance := fmt.Sprintf("# memory.path = %q (source: %s)", result.Config.Memory.Path, wantSource)
			if !strings.Contains(string(rendered), provenance) {
				t.Fatalf("config output missing %q:\n%s", provenance, rendered)
			}
		})
	}
}

func TestMemoryPathValidatesOnlyWinningInput(t *testing.T) {
	dataHome := filepath.Join(t.TempDir(), "data")
	for _, test := range []struct {
		name       string
		tomlPath   string
		env        map[string]string
		flags      []string
		want       string
		wantSource string
		wantError  string
	}{
		{name: "invalid TOML", tomlPath: "relative", wantError: "memory.path must be absolute"},
		{name: "XDG_DATA_HOME overrides invalid TOML", tomlPath: "~", env: map[string]string{"XDG_DATA_HOME": dataHome}, want: filepath.Join(dataHome, "esheep", "memory"), wantSource: "XDG_DATA_HOME"},
		{name: "relative XDG_DATA_HOME overrides valid TOML", tomlPath: "~/valid", env: map[string]string{"XDG_DATA_HOME": "relative"}, wantError: "XDG_DATA_HOME must be absolute"},
		{name: "home-relative XDG_DATA_HOME", env: map[string]string{"XDG_DATA_HOME": "~/data"}, wantError: "XDG_DATA_HOME must be absolute"},
		{name: "Esheep environment overrides relative XDG_DATA_HOME", env: map[string]string{"XDG_DATA_HOME": "relative", "ESHEEP_MEMORY_PATH": "~/memories"}, want: "~/memories", wantSource: "<env>"},
		{name: "flag overrides relative XDG_DATA_HOME", env: map[string]string{"XDG_DATA_HOME": "relative"}, flags: []string{"--memory-path=~/memories"}, want: "~/memories", wantSource: "<pflag>"},
		{name: "flag overrides all invalid paths", tomlPath: "/", env: map[string]string{"XDG_DATA_HOME": "relative", "ESHEEP_MEMORY_PATH": "~another-user"}, flags: []string{"--memory-path=~/memories"}, want: "~/memories", wantSource: "<pflag>"},
		{name: "empty Esheep environment overrides relative XDG_DATA_HOME", env: map[string]string{"XDG_DATA_HOME": "relative", "ESHEEP_MEMORY_PATH": ""}, wantError: "memory.path must not be empty"},
		{name: "same relative value validates as memory path", env: map[string]string{"XDG_DATA_HOME": "relative", "ESHEEP_MEMORY_PATH": "relative"}, wantError: "memory.path must be absolute"},
		{name: "invalid flag overrides valid environment", env: map[string]string{"XDG_DATA_HOME": dataHome, "ESHEEP_MEMORY_PATH": "~/valid"}, flags: []string{"--memory-path=/"}, wantError: "memory.path is too broad"},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			env := testEnv(home, t.TempDir())
			for key, value := range test.env {
				env[key] = value
			}
			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			flags.SetOutput(io.Discard)
			if err := RegisterFlags(flags); err != nil {
				t.Fatal(err)
			}
			if err := flags.Parse(test.flags); err != nil {
				t.Fatal(err)
			}
			options := LoadOptions{Env: env, Flags: flags}
			if test.tomlPath != "" {
				options.ConfigPath = filepath.Join(t.TempDir(), "settings.toml")
				if err := os.WriteFile(options.ConfigPath, fmt.Appendf(nil, "[memory]\npath = %q\n", test.tomlPath), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			result, err := Load(options)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("Load() error = %v, want %q", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.Config.Memory.Path != test.want || result.Report.Updates["memory.path"] != test.wantSource {
				t.Fatalf("memory path = %q, source = %q, want %q / %q", result.Config.Memory.Path, result.Report.Updates["memory.path"], test.want, test.wantSource)
			}
			if !filepath.IsAbs(result.ResolvedMemory) {
				t.Fatalf("resolved memory path is not absolute: %q", result.ResolvedMemory)
			}
		})
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
