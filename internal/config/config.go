// Package config loads esheep's human-owned settings and resolves the paths they name.
package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/jmcampanini/go-config-loader/configloader"
	"github.com/jmcampanini/go-config-loader/configreporter"
	"github.com/jmcampanini/go-config-loader/pflagloader"
	"github.com/spf13/pflag"
)

const (
	appName         = "esheep"
	configName      = "config.toml"
	configFlag      = "config"
	envPrefix       = "esheep"
	configHelp      = "path to the configuration file (replaces discovered files)"
	memoryDirectory = "memory"
)

// Defaults for command-only settings, which are not TOML configuration keys.
const (
	DefaultColor        = "auto"
	DefaultArchiveState = "all"
)

// ClaudeSessions locates Claude Code's session transcripts.
type ClaudeSessions struct {
	Path string `toml:"path" config:"claude-sessions-path" help:"session transcript root for Claude Code"`
}

// CoworkSessions locates Claude Cowork's local conversation storage.
type CoworkSessions struct {
	Path string `toml:"path" config:"claude-cowork-sessions-path" help:"local session root for Claude Cowork; empty disables discovery"`
}

// PiSessions locates Pi's session transcripts.
type PiSessions struct {
	Path string `toml:"path" config:"pi-sessions-path" help:"session transcript root for Pi"`
}

// CodexSessions locates the shared Codex and local ChatGPT Work transcripts.
type CodexSessions struct {
	Home string `toml:"home" config:"codex-home" help:"Codex home containing sessions and archived_sessions for Codex and local ChatGPT Work"`
}

// Machine configures one other machine that runs esheep. Name is its
// hostname and identifies it on the command line and in output; Host,
// Command, and Timeout are optional and default to the name, "esheep", and
// one minute.
type Machine struct {
	Command string `toml:"command"`
	Host    string `toml:"host"`
	Name    string `toml:"name"`
	Timeout string `toml:"timeout"`
}

// Sessions contains the session transcript roots.
type Sessions struct {
	Claude       ClaudeSessions `toml:"claude"`
	ClaudeCowork CoworkSessions `toml:"claude-cowork"`
	Pi           PiSessions     `toml:"pi"`
	Codex        CodexSessions  `toml:"codex"`
}

// Memory locates the root directory 'memory record' appends to.
type Memory struct {
	Path string `toml:"path" config:"memory-path" help:"root directory for recorded memories"`
}

// Config is the complete human-owned esheep configuration.
type Config struct {
	Sessions Sessions  `toml:"sessions"`
	Memory   Memory    `toml:"memory"`
	Machines []Machine `toml:"machines"`
}

// Locations contains the discovered or explicit settings path.
type Locations struct {
	ConfigFile string
}

// LoadOptions controls configuration loading. A nil Env uses the process
// environment. A non-nil Env is hermetic.
type LoadOptions struct {
	ConfigPath string
	Env        map[string]string
	Flags      *pflag.FlagSet
}

// ResolvedCodexSessions contains the canonical Codex home and its transcript directories.
type ResolvedCodexSessions struct {
	ArchivedSessions string
	Home             string
	Sessions         string
}

// ResolvedSessions contains canonical locations for each harness's session storage.
type ResolvedSessions struct {
	Claude       string
	ClaudeCowork string
	Codex        ResolvedCodexSessions
	Pi           string
}

// ResolvedMachine is a machine entry with every default applied.
type ResolvedMachine struct {
	Command string
	Host    string
	Name    string
	Timeout time.Duration
}

// LoadResult is an effective configuration together with provenance and resolved paths.
type LoadResult struct {
	Config           Config
	Locations        Locations
	ResolvedMachines []ResolvedMachine
	ResolvedMemory   string
	ResolvedSessions ResolvedSessions
	Report           configloader.LoadReport
}

// RegisterFlags registers the config file flag and configuration-backed path flags.
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

// Load applies defaults, discovered or explicit TOML, CODEX_HOME and
// XDG_DATA_HOME, ESHEEP_* variables, and parsed flags in that order.
// Machines are configured only by TOML.
func Load(options LoadOptions) (LoadResult, error) {
	env := options.Env
	if env == nil {
		env = configloader.OSEnv()
	}
	home, err := homeFromEnv(env)
	if err != nil {
		return LoadResult{}, err
	}
	locations := locationsFromEnv(env, home)
	defaults := defaults()

	var fileLoader configloader.ConfigLoader[flagConfig]
	explicitConfig := options.ConfigPath != ""
	if !explicitConfig && options.Flags != nil {
		if flag := options.Flags.Lookup(configFlag); flag != nil && flag.Changed {
			options.ConfigPath = flag.Value.String()
			explicitConfig = true
		}
	}
	if explicitConfig {
		normalizedPath, pathErr := expandConfigPath(options.ConfigPath, home)
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
	loaders := []configloader.ConfigLoader[flagConfig]{fileLoader}
	if codexHome := env["CODEX_HOME"]; codexHome != "" {
		loaders = append(loaders, func(base flagConfig) (flagConfig, configloader.LoadReport, error) {
			base.Sessions.Codex.Home = codexHome
			return base, configloader.LoadReport{Updates: configloader.Updates{
				"sessions.codex.home": "CODEX_HOME",
			}}, nil
		})
	}
	if dataHome := env["XDG_DATA_HOME"]; dataHome != "" {
		loaders = append(loaders, func(base flagConfig) (flagConfig, configloader.LoadReport, error) {
			// Keep the input raw until all overrides have been applied.
			base.Memory.Path = dataHome
			return base, configloader.LoadReport{Updates: configloader.Updates{
				"memory.path": "XDG_DATA_HOME",
			}}, nil
		})
	}
	loaders = append(loaders, envLoader)
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
	if report.Updates["memory.path"] == "XDG_DATA_HOME" {
		if !filepath.IsAbs(cfg.Memory.Path) {
			return LoadResult{}, fmt.Errorf("config: XDG_DATA_HOME must be absolute")
		}
		cfg.Memory.Path = filepath.Join(cfg.Memory.Path, appName, memoryDirectory)
	}
	resolvedMemory, err := resolveMemory(cfg.Memory.Path, home)
	if err != nil {
		return LoadResult{}, err
	}
	resolvedSessions, err := resolveSessions(cfg.Sessions, home)
	if err != nil {
		return LoadResult{}, err
	}
	resolvedMachines, err := resolveMachines(cfg.Machines)
	if err != nil {
		return LoadResult{}, err
	}
	return LoadResult{
		Config:           cfg,
		Locations:        locations,
		ResolvedMachines: resolvedMachines,
		ResolvedMemory:   resolvedMemory,
		ResolvedSessions: resolvedSessions,
		Report:           report,
	}, nil
}

// ReportOptions controls effective TOML rendering.
type ReportOptions struct {
	Provenance bool
	Redact     func(LoadResult) LoadResult
}

// Render returns valid, redirectable TOML followed by resolved paths as comments.
func Render(result LoadResult, options ReportOptions) ([]byte, error) {
	if options.Redact != nil {
		result = options.Redact(result)
	}
	reporter := configreporter.New(flagConfig(result.Config), result.Report)
	text, err := reporter.TOML()
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.Write(text)
	if len(text) == 0 || text[len(text)-1] != '\n' {
		b.WriteByte('\n')
	}
	b.WriteString("\n# Resolved paths (application-computed; not configuration)\n")
	writeResolved := func(name, value string) {
		b.WriteString("# ")
		b.WriteString(sanitizeCommentValue(name))
		b.WriteString(" = ")
		b.WriteString(strconv.Quote(sanitizeCommentValue(value)))
		b.WriteByte('\n')
	}
	writeResolved("config_file", result.Locations.ConfigFile)
	writeResolved("sessions.claude.path", result.ResolvedSessions.Claude)
	writeResolved("sessions.claude-cowork.path", result.ResolvedSessions.ClaudeCowork)
	writeResolved("sessions.pi.path", result.ResolvedSessions.Pi)
	writeResolved("sessions.codex.home", result.ResolvedSessions.Codex.Home)
	writeResolved("sessions.codex.sessions", result.ResolvedSessions.Codex.Sessions)
	writeResolved("sessions.codex.archived_sessions", result.ResolvedSessions.Codex.ArchivedSessions)
	writeResolved("memory.path", result.ResolvedMemory)
	for _, machine := range result.ResolvedMachines {
		writeResolved("machines."+machine.Name+".host", machine.Host)
		writeResolved("machines."+machine.Name+".command", machine.Command)
		writeResolved("machines."+machine.Name+".timeout", machine.Timeout.String())
	}
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
	var coworkPath string
	if runtime.GOOS == "darwin" {
		coworkPath = "~/Library/Application Support/Claude/local-agent-mode-sessions"
	}
	return flagConfig{
		Sessions: Sessions{
			Claude:       ClaudeSessions{Path: "~/.claude/projects"},
			ClaudeCowork: CoworkSessions{Path: coworkPath},
			Pi:           PiSessions{Path: "~/.pi/agent/sessions"},
			Codex:        CodexSessions{Home: "~/.codex"},
		},
		Memory: Memory{Path: "~/.local/share/" + appName + "/" + memoryDirectory},
	}
}

func homeFromEnv(env map[string]string) (string, error) {
	home := env["HOME"]
	if home == "" {
		return "", fmt.Errorf("config: HOME must be set")
	}
	if !filepath.IsAbs(home) {
		return "", fmt.Errorf("config: HOME must be absolute")
	}
	resolved, err := canonicalPath(home)
	if err != nil {
		return "", fmt.Errorf("config: resolve HOME: %w", err)
	}
	return resolved, nil
}

// locationsFromEnv places the discovered configuration file under
// XDG_CONFIG_HOME, falling back to ~/.config when the variable is unset,
// empty, or relative, which the XDG Base Directory specification treats as
// invalid.
func locationsFromEnv(env map[string]string, home string) Locations {
	configHome := env["XDG_CONFIG_HOME"]
	if !filepath.IsAbs(configHome) {
		configHome = filepath.Join(home, ".config")
	}
	return Locations{ConfigFile: filepath.Join(filepath.Clean(configHome), appName, configName)}
}

// resolveMemory resolves the memory root, the one location esheep writes,
// and rejects the filesystem root and the home directory as too broad.
func resolveMemory(path, home string) (string, error) {
	memory, err := resolveManagedPath("memory.path", path, home)
	if err != nil {
		return "", err
	}
	if memory == string(filepath.Separator) || strings.EqualFold(memory, home) {
		return "", errors.New("config: memory.path is too broad")
	}
	return memory, nil
}

// resolveSessions resolves the read-only session transcript roots. Session
// roots are harness-owned inputs, so unlike the memory root they need no
// breadth restriction.
func resolveSessions(cfg Sessions, home string) (ResolvedSessions, error) {
	codexHome, err := resolveManagedPath("sessions.codex.home", cfg.Codex.Home, home)
	if err != nil {
		return ResolvedSessions{}, err
	}
	resolved := ResolvedSessions{Codex: ResolvedCodexSessions{Home: codexHome}}
	if cfg.ClaudeCowork.Path != "" {
		resolved.ClaudeCowork, err = resolveManagedPath("sessions.claude-cowork.path", cfg.ClaudeCowork.Path, home)
		if err != nil {
			return ResolvedSessions{}, err
		}
	}
	configured := []struct {
		name     string
		path     string
		resolved *string
	}{
		{name: "claude.path", path: cfg.Claude.Path, resolved: &resolved.Claude},
		{name: "pi.path", path: cfg.Pi.Path, resolved: &resolved.Pi},
		{name: "codex.sessions", path: filepath.Join(codexHome, "sessions"), resolved: &resolved.Codex.Sessions},
		{name: "codex.archived_sessions", path: filepath.Join(codexHome, "archived_sessions"), resolved: &resolved.Codex.ArchivedSessions},
	}
	for _, root := range configured {
		path, err := resolveManagedPath("sessions."+root.name, root.path, home)
		if err != nil {
			return ResolvedSessions{}, err
		}
		*root.resolved = path
	}
	return resolved, nil
}

// Machine entry defaults.
const (
	defaultMachineCommand = "esheep"
	defaultMachineTimeout = time.Minute
)

// resolveMachines validates machine entries and applies their defaults.
// Names are hostnames, so they compare case-insensitively; "all" is reserved
// for the --remote selector.
func resolveMachines(configured []Machine) ([]ResolvedMachine, error) {
	resolved := make([]ResolvedMachine, 0, len(configured))
	for _, machine := range configured {
		if machine.Name == "" || strings.ContainsAny(machine.Name, ", \t\r\n") {
			return nil, fmt.Errorf("config: machine name %q must be a non-empty hostname without commas or whitespace", machine.Name)
		}
		if strings.EqualFold(machine.Name, "all") {
			return nil, errors.New(`config: machine name "all" is reserved`)
		}
		for _, prior := range resolved {
			if strings.EqualFold(prior.Name, machine.Name) {
				return nil, fmt.Errorf("config: duplicate machine name %q", machine.Name)
			}
		}

		entry := ResolvedMachine{Command: defaultMachineCommand, Host: machine.Name, Name: machine.Name, Timeout: defaultMachineTimeout}
		if machine.Host != "" {
			entry.Host = machine.Host
		}
		if machine.Command != "" {
			entry.Command = machine.Command
		}
		if machine.Timeout != "" {
			timeout, err := time.ParseDuration(machine.Timeout)
			if err != nil {
				return nil, fmt.Errorf("config: machine %q timeout: %w", machine.Name, err)
			}
			if timeout <= 0 {
				return nil, fmt.Errorf("config: machine %q timeout must be positive", machine.Name)
			}
			entry.Timeout = timeout
		}
		resolved = append(resolved, entry)
	}
	return resolved, nil
}

func resolveManagedPath(name, path, home string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("config: %s must not be empty", name)
	}
	expanded, err := expandHome(path, home)
	if err != nil {
		return "", fmt.Errorf("config: %s: %w", name, err)
	}
	if !filepath.IsAbs(expanded) {
		return "", fmt.Errorf("config: %s must be absolute or start with ~/", name)
	}
	resolved, err := canonicalPath(expanded)
	if err != nil {
		return "", fmt.Errorf("config: %s: %w", name, err)
	}
	return resolved, nil
}

func expandConfigPath(path, home string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("config: file path is empty")
	}
	expanded, err := expandHome(path, home)
	if err != nil {
		return "", err
	}
	if absolute, absErr := filepath.Abs(expanded); absErr == nil {
		return filepath.Clean(absolute), nil
	}
	return "", fmt.Errorf("config: resolve file path %q", path)
}

func expandHome(path, home string) (string, error) {
	if path == "~" {
		return home, nil
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:]), nil
	}
	if strings.HasPrefix(path, "~") {
		return "", fmt.Errorf("unsupported user-home path %q", path)
	}
	return path, nil
}

func canonicalPath(path string) (string, error) {
	clean := filepath.Clean(path)
	current := clean
	var missing []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("resolve path %q: %w", clean, err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("resolve path %q: %w", clean, err)
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func sanitizeCommentValue(value string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(value)
}
