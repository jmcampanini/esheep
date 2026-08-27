// Package config loads esheep's human-owned settings and derived filesystem locations.
package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jmcampanini/go-config-loader/configloader"
	"github.com/jmcampanini/go-config-loader/configreporter"
	"github.com/jmcampanini/go-config-loader/pflagloader"
	"github.com/spf13/pflag"
)

const (
	appName    = "esheep"
	configName = "esheep.toml"
	configFlag = "config"
	envPrefix  = "esheep"
	configHelp = "path to the configuration file (replaces discovered files)"
)

// ClaudeTarget configures the Claude Code skill target.
type ClaudeTarget struct {
	Enabled bool   `toml:"enabled" config:"claude-enabled" help:"enable the Claude target"`
	Path    string `toml:"path" config:"claude-path" help:"installation path for the Claude target"`
}

// PiTarget configures the Pi skill target.
type PiTarget struct {
	Enabled bool   `toml:"enabled" config:"pi-enabled" help:"enable the Pi target"`
	Path    string `toml:"path" config:"pi-path" help:"installation path for the Pi target"`
}

// CodexTarget configures the Codex skill target.
type CodexTarget struct {
	Enabled bool   `toml:"enabled" config:"codex-enabled" help:"enable the Codex target"`
	Path    string `toml:"path" config:"codex-path" help:"installation path for the Codex target"`
}

// AgentsTarget configures the shared Agent Skills target.
type AgentsTarget struct {
	Enabled bool   `toml:"enabled" config:"agents-enabled" help:"enable the shared agents target"`
	Path    string `toml:"path" config:"agents-path" help:"installation path for the shared agents target"`
}

// Targets contains the four supported installation targets.
type Targets struct {
	Claude ClaudeTarget `toml:"claude"`
	Pi     PiTarget     `toml:"pi"`
	Codex  CodexTarget  `toml:"codex"`
	Agents AgentsTarget `toml:"agents"`
}

// Config is the complete human-owned esheep configuration.
type Config struct {
	Targets Targets `toml:"targets"`
}

// Locations are machine-derived paths. They are not configuration-file keys.
// Relative XDG_* values are made absolute from the current working directory,
// matching go-config-loader's current behavior.
type Locations struct {
	ConfigFile string
	Registry   string
	CloneRoot  string
}

// LoadOptions controls configuration loading. A nil Env uses the process
// environment. Flags may be nil when only defaults, files, and environment
// variables are needed.
type LoadOptions struct {
	ConfigPath string
	Env        map[string]string
	Flags      *pflag.FlagSet
}

// ResolvedTargets contains absolute target paths for runtime operations.
type ResolvedTargets struct {
	Claude string
	Pi     string
	Codex  string
	Agents string
}

// LoadResult is an effective configuration together with provenance and paths.
type LoadResult struct {
	Config          Config
	Locations       Locations
	ResolvedTargets ResolvedTargets
	Report          configloader.LoadReport
}

// RegisterFlags registers the config file flag and all configuration-backed
// flags. It must be called before the flag set is parsed.
func RegisterFlags(flags *pflag.FlagSet) error {
	if flags == nil {
		return fmt.Errorf("config: flag set is nil")
	}
	if flags.Lookup(configFlag) != nil {
		return fmt.Errorf("config: flag %q already exists", configFlag)
	}
	flags.String(configFlag, "", configHelp)
	return pflagloader.Register[flagConfig](flags)
}

// Load applies defaults, discovered or explicit TOML, ESHEEP_* variables, and
// parsed flags in that order. An explicit ConfigPath is required and replaces
// all discovered files.
func Load(options LoadOptions) (LoadResult, error) {
	env := options.Env
	if env == nil {
		env = configloader.OSEnv()
	}
	locations := locationsFromEnv(env)
	defaults := defaults()

	var fileLoader configloader.ConfigLoader[flagConfig]
	var err error
	explicitConfig := options.ConfigPath != ""
	if !explicitConfig && options.Flags != nil {
		if flag := options.Flags.Lookup(configFlag); flag != nil && flag.Changed {
			options.ConfigPath = flag.Value.String()
			explicitConfig = true
		}
	}
	if explicitConfig {
		normalizedPath, pathErr := expandPath(options.ConfigPath, homeFromEnv(env))
		if pathErr != nil {
			return LoadResult{}, pathErr
		}
		fileLoader, err = configloader.NewRequiredFileLoader[flagConfig](normalizedPath)
		if err == nil {
			locations.ConfigFile = normalizedPath
		}
	} else {
		fileLoader, err = configloader.NewMergeAllFilesLoader[flagConfig]([]string{locations.ConfigFile})
	}
	if err != nil {
		return LoadResult{}, err
	}

	envLoader, err := configloader.NewEnvironmentLoader[flagConfig](envPrefix, env)
	if err != nil {
		return LoadResult{}, err
	}
	loaders := []configloader.ConfigLoader[flagConfig]{fileLoader, envLoader}
	if options.Flags != nil {
		flagLoader, loaderErr := pflagloader.NewLoader[flagConfig](options.Flags)
		if loaderErr != nil {
			return LoadResult{}, loaderErr
		}
		loaders = append(loaders, flagLoader)
	}
	loaded, report, err := configloader.Load(defaults, loaders...)
	if err != nil {
		return LoadResult{}, err
	}
	cfg := Config(loaded)
	resolved, err := resolveTargets(cfg, homeFromEnv(env))
	if err != nil {
		return LoadResult{}, err
	}
	return LoadResult{Config: cfg, Locations: locations, ResolvedTargets: resolved, Report: report}, nil
}

// ReportOptions controls effective TOML rendering.
type ReportOptions struct {
	Provenance bool
	Redact     func(LoadResult) LoadResult
}

// Render returns valid, redirectable TOML followed by derived values as
// comments. Provenance is also emitted as comments when requested.
func Render(result LoadResult, options ReportOptions) ([]byte, error) {
	if options.Redact != nil {
		result = options.Redact(result)
	}
	cfg := result.Config
	reporter := configreporter.New(flagConfig(cfg), result.Report)
	text, err := reporter.TOML()
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.Write(text)
	if len(text) == 0 || text[len(text)-1] != '\n' {
		b.WriteByte('\n')
	}
	b.WriteString("\n# Derived values (application-computed; not configuration)\n")
	writeDerived := func(name, value string) {
		b.WriteString("# ")
		b.WriteString(sanitizeCommentValue(name))
		b.WriteString(" = ")
		b.WriteString(strconv.Quote(sanitizeCommentValue(value)))
		b.WriteByte('\n')
	}
	writeDerived("config_file", result.Locations.ConfigFile)
	writeDerived("registry", result.Locations.Registry)
	writeDerived("clone_root", result.Locations.CloneRoot)
	writeDerived("targets.claude.path", result.ResolvedTargets.Claude)
	writeDerived("targets.pi.path", result.ResolvedTargets.Pi)
	writeDerived("targets.codex.path", result.ResolvedTargets.Codex)
	writeDerived("targets.agents.path", result.ResolvedTargets.Agents)
	if options.Provenance {
		b.WriteString("\n# Provenance\n")
		for _, row := range reporter.ProvenanceRows() {
			b.WriteString("# ")
			b.WriteString(sanitizeCommentValue(row[0]))
			b.WriteString(" = ")
			b.WriteString(sanitizeCommentValue(row[1]))
			b.WriteString(" (source: ")
			b.WriteString(sanitizeCommentValue(row[2]))
			b.WriteString(")\n")
		}
	}
	return []byte(b.String()), nil
}

// WriteReport writes Render's output to w.
func WriteReport(w io.Writer, result LoadResult, options ReportOptions) error {
	if w == nil {
		return fmt.Errorf("config: writer is nil")
	}
	text, err := Render(result, options)
	if err != nil {
		return err
	}
	_, err = w.Write(text)
	return err
}

type flagConfig Config

func defaults() flagConfig {
	return flagConfig{Targets: Targets{
		Claude: ClaudeTarget{Enabled: true, Path: "~/.claude/skills"},
		Pi:     PiTarget{Enabled: true, Path: "~/.pi/agent/skills"},
		Codex:  CodexTarget{Enabled: true, Path: "~/.codex/skills"},
		Agents: AgentsTarget{Enabled: false, Path: "~/.agents/skills"},
	}}
}

func locationsFromEnv(env map[string]string) Locations {
	home := homeFromEnv(env)
	configHome := xdgHome(env, "XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	stateHome := xdgHome(env, "XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	dataHome := xdgHome(env, "XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	return Locations{
		ConfigFile: filepath.Join(configHome, appName, configName),
		Registry:   filepath.Join(stateHome, appName, "repos.toml"),
		CloneRoot:  filepath.Join(dataHome, appName, "repos"),
	}
}

func homeFromEnv(env map[string]string) string {
	if home := env["HOME"]; home != "" {
		return absoluteClean(home)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return absoluteClean(home)
}

func xdgHome(env map[string]string, key, fallback string) string {
	if value := env[key]; value != "" {
		return absoluteClean(value)
	}
	return absoluteClean(fallback)
}

func absoluteClean(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}

func resolveTargets(cfg Config, home string) (ResolvedTargets, error) {
	claude, err := resolveTargetPath("claude", cfg.Targets.Claude.Path, home)
	if err != nil {
		return ResolvedTargets{}, err
	}
	pi, err := resolveTargetPath("pi", cfg.Targets.Pi.Path, home)
	if err != nil {
		return ResolvedTargets{}, err
	}
	codex, err := resolveTargetPath("codex", cfg.Targets.Codex.Path, home)
	if err != nil {
		return ResolvedTargets{}, err
	}
	agents, err := resolveTargetPath("agents", cfg.Targets.Agents.Path, home)
	if err != nil {
		return ResolvedTargets{}, err
	}
	return ResolvedTargets{Claude: claude, Pi: pi, Codex: codex, Agents: agents}, nil
}

func resolveTargetPath(name, path, home string) (string, error) {
	resolved, err := expandPath(path, home)
	if err != nil {
		return "", fmt.Errorf("config: targets.%s.path: %w", name, err)
	}
	if resolved == "" {
		return "", fmt.Errorf("config: targets.%s.path must not be empty", name)
	}
	return resolved, nil
}

func sanitizeCommentValue(value string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(value)
}

func expandPath(path, home string) (string, error) {
	if path == "" {
		return "", nil
	}
	if path == "~" {
		path = home
	} else if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		if home == "" {
			return "", fmt.Errorf("cannot expand %q without a home directory", path)
		}
		path = filepath.Join(home, path[2:])
	} else if strings.HasPrefix(path, "~") {
		return "", fmt.Errorf("unsupported user-home path %q", path)
	}
	return absoluteClean(path), nil
}
