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
		"HOME":                  home,
		"XDG_CONFIG_HOME":       configHome,
		"ESHEEP_CLAUDE_ENABLED": "true",
	}
}

func TestDefaultsAndLocation(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	configHome := filepath.Join(t.TempDir(), "config")
	got := defaults()
	if !got.Targets.Claude.Enabled || !got.Targets.Pi.Enabled || !got.Targets.Codex.Enabled {
		t.Fatalf("default enabled targets = %#v", got.Targets)
	}
	if got.Targets.Claude.Path != "~/.claude/skills" || got.Targets.Pi.Path != "~/.pi/agent/skills" || got.Targets.Codex.Path != "~/.agents/skills" {
		t.Fatalf("default target paths = %#v", got.Targets)
	}
	location, err := locationsFromEnv(testEnv(home, configHome), home)
	if err != nil {
		t.Fatal(err)
	}
	if location.ConfigFile != filepath.Join(configHome, "esheep", "esheep.toml") {
		t.Fatalf("location = %#v", location)
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
		{name: "relative xdg", env: map[string]string{"HOME": absoluteHome, "XDG_CONFIG_HOME": "config"}},
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
	if err := os.WriteFile(filepath.Join(cwd, "esheep.toml"), []byte("[targets.claude]\nenabled = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := testEnv(filepath.Join(t.TempDir(), "home"), filepath.Join(t.TempDir(), "config"))
	result, err := Load(LoadOptions{Env: env})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Config.Targets.Claude.Enabled || len(result.Report.LoadedFiles) != 0 {
		t.Fatalf("cwd config affected result = %#v report = %#v", result.Config.Targets, result.Report)
	}
}

func TestSourcesLoadOnlyFromTOMLAndResolve(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	configHome := filepath.Join(root, "config")
	first := filepath.Join(root, "personal")
	second := filepath.Join(root, "work")
	for _, path := range []string{home, first, second, filepath.Join(configHome, "esheep")} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	settings := "[[sources]]\nname = \"personal\"\npath = \"" + first + "\"\n\n[[sources]]\nname = \"work\"\npath = \"" + second + "\"\n"
	path := filepath.Join(configHome, "esheep", "esheep.toml")
	if err := os.WriteFile(path, []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Load(LoadOptions{Env: testEnv(home, configHome)})
	if err != nil {
		t.Fatal(err)
	}
	canonicalFirst, err := canonicalPath(first)
	if err != nil {
		t.Fatal(err)
	}
	canonicalSecond, err := canonicalPath(second)
	if err != nil {
		t.Fatal(err)
	}
	want := []ResolvedSource{{Name: "personal", Path: canonicalFirst}, {Name: "work", Path: canonicalSecond}}
	if !reflect.DeepEqual(result.ResolvedSources, want) {
		t.Fatalf("resolved sources = %#v, want %#v", result.ResolvedSources, want)
	}
	if result.Report.Updates["sources"] != path {
		t.Fatalf("source provenance = %#v", result.Report.Updates)
	}
}

func TestApprovedEnvironmentNamesAndFlagsLoadEachTarget(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	env := testEnv(home, filepath.Join(t.TempDir(), "config"))
	env["ESHEEP_PI_ENABLED"] = "false"
	env["ESHEEP_CODEX_PATH"] = "~/codex-overridden"
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.SetOutput(io.Discard)
	if err := RegisterFlags(flags); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"claude-enabled", "claude-path", "pi-enabled", "pi-path", "codex-enabled", "codex-path"} {
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
	wantClaude, err := canonicalPath(filepath.Join(home, "from-flag"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Config.Targets.Claude.Enabled || result.ResolvedTargets.Claude != wantClaude || result.Config.Targets.Pi.Enabled {
		t.Fatalf("effective targets = %#v resolved = %#v", result.Config.Targets, result.ResolvedTargets)
	}
	if result.Report.Updates["targets.claude.path"] != "<pflag>" || result.Report.Updates["targets.pi.enabled"] != "<env>" {
		t.Fatalf("provenance = %#v", result.Report.Updates)
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
	if err := os.WriteFile(explicit, []byte("[targets.pi]\nenabled = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Load(LoadOptions{Env: env, ConfigPath: explicit})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Config.Targets.Claude.Enabled || result.Config.Targets.Pi.Enabled || result.Locations.ConfigFile != explicit {
		t.Fatalf("explicit result = %#v", result)
	}
}

func TestManagedPathsMustBeDisjointAndAbsolute(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	base := Config(defaults())
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "relative source", mutate: func(cfg *Config) { cfg.Sources = []Source{{Name: "one", Path: "skills"}} }},
		{name: "relative target", mutate: func(cfg *Config) { cfg.Targets.Claude.Path = "skills" }},
		{name: "nested sources", mutate: func(cfg *Config) {
			cfg.Sources = []Source{{Name: "one", Path: filepath.Join(root, "sources")}, {Name: "two", Path: filepath.Join(root, "sources", "nested")}}
		}},
		{name: "duplicate names ignoring case", mutate: func(cfg *Config) {
			cfg.Sources = []Source{{Name: "One", Path: filepath.Join(root, "one")}, {Name: "one", Path: filepath.Join(root, "two")}}
		}},
		{name: "overlapping targets", mutate: func(cfg *Config) { cfg.Targets.Pi.Path = cfg.Targets.Claude.Path }},
		{name: "source target overlap", mutate: func(cfg *Config) {
			cfg.Sources = []Source{{Name: "one", Path: filepath.Join(home, ".claude")}}
		}},
		{name: "unicode-equivalent source target overlap", mutate: func(cfg *Config) {
			cfg.Sources = []Source{{Name: "one", Path: filepath.Join(root, "caf\u00e9")}}
			cfg.Targets.Claude.Path = filepath.Join(root, "cafe\u0301")
		}},
		{name: "home target", mutate: func(cfg *Config) { cfg.Targets.Claude.Path = home }},
		{name: "root target", mutate: func(cfg *Config) { cfg.Targets.Claude.Path = string(filepath.Separator) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := base
			test.mutate(&cfg)
			if _, _, err := resolvePaths(cfg, home); err == nil {
				t.Fatal("resolvePaths succeeded")
			}
		})
	}
}

func TestManagedPathComparisonUsesUnicodeCaseFolding(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sigma := filepath.Join(root, "σ")
	finalSigma := filepath.Join(root, "ς")
	if !samePath(sigma, finalSigma) {
		t.Fatalf("samePath(%q, %q) = false", sigma, finalSigma)
	}
	if !pathContains(sigma, filepath.Join(finalSigma, "target")) {
		t.Fatalf("pathContains(%q, %q) = false", sigma, filepath.Join(finalSigma, "target"))
	}
}

func TestMissingPathResolvesExistingSymlinkedParent(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	realParent := filepath.Join(root, "real")
	sourcePath := filepath.Join(realParent, "source")
	if err := os.MkdirAll(sourcePath, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(realParent, alias); err != nil {
		t.Fatal(err)
	}
	cfg := Config(defaults())
	cfg.Sources = []Source{{Name: "one", Path: sourcePath}}
	cfg.Targets.Claude.Path = filepath.Join(alias, "source", "missing-target")
	if _, _, err := resolvePaths(cfg, home); err == nil {
		t.Fatal("overlap through a symlinked parent succeeded")
	}
}

func TestEnabledTargetRootMustNotBeSymlink(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	realTarget := filepath.Join(root, "target")
	if err := os.MkdirAll(realTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "target-alias")
	if err := os.Symlink(realTarget, alias); err != nil {
		t.Fatal(err)
	}
	cfg := Config(defaults())
	cfg.Targets.Claude.Path = alias
	if _, _, err := resolvePaths(cfg, home); err == nil {
		t.Fatal("symlinked target succeeded")
	}
}

func TestSourceSymlinksUseCanonicalBoundary(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	realSource := filepath.Join(root, "source")
	if err := os.MkdirAll(realSource, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(realSource, alias); err != nil {
		t.Fatal(err)
	}
	cfg := Config(defaults())
	cfg.Sources = []Source{{Name: "one", Path: realSource}, {Name: "two", Path: alias}}
	if _, _, err := resolvePaths(cfg, home); err == nil {
		t.Fatal("aliased sources succeeded")
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

func TestProfilesResolveWithPrecedenceEnvProfilesAndDedupe(t *testing.T) {
	env := testEnv(filepath.Join(t.TempDir(), "home"), filepath.Join(t.TempDir(), "config"))
	env["MACHINE_PROFILES"] = " client ,alpha,"
	path := filepath.Join(t.TempDir(), "esheep.toml")
	if err := os.WriteFile(path, []byte("profiles = [\"file-profile\"]\nenv_profiles = [\"MACHINE_PROFILES\", \"UNSET_PROFILES\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.SetOutput(io.Discard)
	if err := RegisterFlags(flags); err != nil {
		t.Fatal(err)
	}
	if err := flags.Parse([]string{"--profiles=alpha,beta", "--profile", "gamma"}); err != nil {
		t.Fatal(err)
	}

	result, err := Load(LoadOptions{Env: env, ConfigPath: path, Flags: flags})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"alpha", "beta", "gamma", "client"}
	if !reflect.DeepEqual(result.EffectiveProfiles, want) {
		t.Fatalf("EffectiveProfiles = %#v, want %#v", result.EffectiveProfiles, want)
	}
}

func TestProfilesLoadFromEnvironmentVariable(t *testing.T) {
	env := testEnv(filepath.Join(t.TempDir(), "home"), filepath.Join(t.TempDir(), "config"))
	env["ESHEEP_PROFILES"] = "work,client"

	result, err := Load(LoadOptions{Env: env})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"work", "client"}
	if !reflect.DeepEqual(result.EffectiveProfiles, want) {
		t.Fatalf("EffectiveProfiles = %#v, want %#v", result.EffectiveProfiles, want)
	}
}

func TestProfilesRejectInvalidAndReservedNames(t *testing.T) {
	tests := []struct {
		name string
		toml string
		env  map[string]string
	}{
		{name: "reserved profile", toml: "profiles = [\"base\"]\n"},
		{name: "invalid grammar", toml: "profiles = [\"Work\"]\n"},
		{name: "padded env_profiles entry", toml: "env_profiles = [\" MACHINE\"]\n"},
		{name: "blank env_profiles entry", toml: "env_profiles = [\"\"]\n"},
		{name: "invalid appended profile", toml: "env_profiles = [\"MACHINE\"]\n", env: map[string]string{"MACHINE": "no.dots"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := testEnv(filepath.Join(t.TempDir(), "home"), filepath.Join(t.TempDir(), "config"))
			for key, value := range test.env {
				env[key] = value
			}
			path := filepath.Join(t.TempDir(), "esheep.toml")
			if err := os.WriteFile(path, []byte(test.toml), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(LoadOptions{Env: env, ConfigPath: path}); err == nil {
				t.Fatal("Load accepted an invalid profile configuration")
			}
		})
	}
}

func TestRenderReportsEffectiveProfiles(t *testing.T) {
	env := testEnv(filepath.Join(t.TempDir(), "home"), filepath.Join(t.TempDir(), "config"))
	env["ESHEEP_PROFILES"] = "work"
	result, err := Load(LoadOptions{Env: env})
	if err != nil {
		t.Fatal(err)
	}
	output, err := Render(result, ReportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "# effective_profiles = [\"work\"]") {
		t.Fatalf("report = %s", output)
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
	if strings.Contains(string(output), "sensitive-value") || !strings.Contains(string(output), "# targets.claude.path = \"REDACTED\"") || !strings.Contains(string(output), "# Resolved paths") || strings.Contains(string(output), "source\r\n") {
		t.Fatalf("report = %s", output)
	}
	var decoded Config
	if _, err := toml.Decode(string(output), &decoded); err != nil {
		t.Fatalf("rendered output is not TOML: %v\n%s", err, output)
	}
}
