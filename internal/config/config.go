// Package config loads esheep's human-owned settings and resolves filesystem boundaries.
package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jmcampanini/esheep/internal/naming"
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

// Source configures one human-managed directory containing skills.
type Source struct {
	Name string `toml:"name"`
	Path string `toml:"path"`
}

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

// ClaudeSessions locates Claude Code's session transcripts.
type ClaudeSessions struct {
	Path string `toml:"path" config:"claude-sessions-path" help:"session transcript root for Claude Code"`
}

// PiSessions locates Pi's session transcripts.
type PiSessions struct {
	Path string `toml:"path" config:"pi-sessions-path" help:"session transcript root for Pi"`
}

// CodexSessions locates Codex's session transcripts.
type CodexSessions struct {
	Path string `toml:"path" config:"codex-sessions-path" help:"session transcript root for Codex"`
}

// Sessions contains the per-harness session transcript roots.
type Sessions struct {
	Claude ClaudeSessions `toml:"claude"`
	Pi     PiSessions     `toml:"pi"`
	Codex  CodexSessions  `toml:"codex"`
}

// Config is the complete human-owned esheep configuration.
type Config struct {
	Profiles    []string `toml:"profiles" config:"profiles" pflag_singular:"profile" help:"active profiles; --profiles accepts comma-separated values."`
	EnvProfiles []string `toml:"env_profiles"`
	Sources     []Source `toml:"sources"`
	Targets     Targets  `toml:"targets"`
	Sessions    Sessions `toml:"sessions"`
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

// ResolvedSource is a source identity with an absolute canonical path.
type ResolvedSource struct {
	Name string
	Path string
}

// ResolvedTargets contains absolute target paths for runtime operations.
type ResolvedTargets struct {
	Claude string
	Pi     string
	Codex  string
	Agents string
}

// ResolvedSessions contains absolute session transcript roots.
type ResolvedSessions struct {
	Claude string
	Pi     string
	Codex  string
}

// LoadResult is an effective configuration together with provenance and resolved paths.
type LoadResult struct {
	Config            Config
	EffectiveProfiles []string
	Locations         Locations
	ResolvedSessions  ResolvedSessions
	ResolvedSources   []ResolvedSource
	ResolvedTargets   ResolvedTargets
	Report            configloader.LoadReport
}

// RegisterFlags registers the config file flag and configuration-backed target flags.
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
// parsed flags in that order. Sources are configured only by TOML.
func Load(options LoadOptions) (LoadResult, error) {
	env := options.Env
	if env == nil {
		env = configloader.OSEnv()
	}
	home, err := homeFromEnv(env)
	if err != nil {
		return LoadResult{}, err
	}
	locations, err := locationsFromEnv(env, home)
	if err != nil {
		return LoadResult{}, err
	}
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
	effectiveProfiles, err := resolveProfiles(cfg, env)
	if err != nil {
		return LoadResult{}, err
	}
	resolvedSources, resolvedTargets, err := resolvePaths(cfg, home)
	if err != nil {
		return LoadResult{}, err
	}
	resolvedSessions, err := resolveSessions(cfg.Sessions, home)
	if err != nil {
		return LoadResult{}, err
	}
	return LoadResult{
		Config:            cfg,
		EffectiveProfiles: effectiveProfiles,
		Locations:         locations,
		ResolvedSessions:  resolvedSessions,
		ResolvedSources:   resolvedSources,
		ResolvedTargets:   resolvedTargets,
		Report:            report,
	}, nil
}

// resolveProfiles appends the comma-separated values of each env_profiles
// variable to the loaded profile list, dedupes preserving first-seen order,
// and validates every name.
func resolveProfiles(cfg Config, env map[string]string) ([]string, error) {
	for _, name := range cfg.EnvProfiles {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" || trimmed != name {
			return nil, fmt.Errorf("config: env_profiles entry %q must be a nonblank environment variable name", name)
		}
	}

	profiles := append([]string(nil), cfg.Profiles...)
	for _, name := range cfg.EnvProfiles {
		for _, value := range strings.Split(env[name], ",") {
			if value = strings.TrimSpace(value); value != "" {
				profiles = append(profiles, value)
			}
		}
	}

	seen := make(map[string]struct{}, len(profiles))
	effective := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		if err := naming.ValidateProfileName(profile); err != nil {
			return nil, fmt.Errorf("config: %w", err)
		}
		if _, duplicate := seen[profile]; duplicate {
			continue
		}
		seen[profile] = struct{}{}
		effective = append(effective, profile)
	}
	return effective, nil
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
	quoted := make([]string, 0, len(result.EffectiveProfiles))
	for _, profile := range result.EffectiveProfiles {
		quoted = append(quoted, strconv.Quote(sanitizeCommentValue(profile)))
	}
	b.WriteString("# effective_profiles = [")
	b.WriteString(strings.Join(quoted, ", "))
	b.WriteString("]\n")
	for _, source := range result.ResolvedSources {
		writeResolved("sources."+source.Name+".path", source.Path)
	}
	writeResolved("targets.claude.path", result.ResolvedTargets.Claude)
	writeResolved("targets.pi.path", result.ResolvedTargets.Pi)
	writeResolved("targets.codex.path", result.ResolvedTargets.Codex)
	writeResolved("targets.agents.path", result.ResolvedTargets.Agents)
	writeResolved("sessions.claude.path", result.ResolvedSessions.Claude)
	writeResolved("sessions.pi.path", result.ResolvedSessions.Pi)
	writeResolved("sessions.codex.path", result.ResolvedSessions.Codex)
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
	return flagConfig{
		Targets: Targets{
			Claude: ClaudeTarget{Enabled: true, Path: "~/.claude/skills"},
			Pi:     PiTarget{Enabled: true, Path: "~/.pi/agent/skills"},
			Codex:  CodexTarget{Enabled: true, Path: "~/.codex/skills"},
			Agents: AgentsTarget{Enabled: false, Path: "~/.agents/skills"},
		},
		Sessions: Sessions{
			Claude: ClaudeSessions{Path: "~/.claude/projects"},
			Pi:     PiSessions{Path: "~/.pi/agent/sessions"},
			Codex:  CodexSessions{Path: "~/.codex/sessions"},
		},
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

func locationsFromEnv(env map[string]string, home string) (Locations, error) {
	configHome := env["XDG_CONFIG_HOME"]
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	} else if !filepath.IsAbs(configHome) {
		return Locations{}, fmt.Errorf("config: XDG_CONFIG_HOME must be absolute")
	}
	return Locations{ConfigFile: filepath.Join(filepath.Clean(configHome), appName, configName)}, nil
}

func resolvePaths(cfg Config, home string) ([]ResolvedSource, ResolvedTargets, error) {
	sources, err := resolveSources(cfg.Sources, home)
	if err != nil {
		return nil, ResolvedTargets{}, err
	}
	targets, enabled, err := resolveTargets(cfg.Targets, home)
	if err != nil {
		return nil, ResolvedTargets{}, err
	}
	for _, source := range sources {
		for _, target := range enabled {
			if pathsOverlap(source.Path, target.path) {
				return nil, ResolvedTargets{}, fmt.Errorf("config: source %q overlaps enabled target %q", source.Name, target.name)
			}
		}
	}
	return sources, targets, nil
}

func resolveSources(configured []Source, home string) ([]ResolvedSource, error) {
	resolved := make([]ResolvedSource, 0, len(configured))
	for _, source := range configured {
		if err := naming.ValidateSourceName(source.Name); err != nil {
			return nil, fmt.Errorf("config: %w", err)
		}
		path, err := resolveManagedPath("source "+strconv.Quote(source.Name), source.Path, home)
		if err != nil {
			return nil, err
		}
		for _, prior := range resolved {
			if strings.EqualFold(prior.Name, source.Name) {
				return nil, fmt.Errorf("config: duplicate source name %q", source.Name)
			}
			if pathsOverlap(prior.Path, path) {
				return nil, fmt.Errorf("config: source %q overlaps source %q", source.Name, prior.Name)
			}
		}
		resolved = append(resolved, ResolvedSource{Name: source.Name, Path: path})
	}
	return resolved, nil
}

type resolvedTarget struct {
	name string
	path string
}

func resolveTargets(cfg Targets, home string) (ResolvedTargets, []resolvedTarget, error) {
	resolved := ResolvedTargets{}
	configured := []struct {
		name     string
		enabled  bool
		path     string
		resolved *string
	}{
		{name: "claude", enabled: cfg.Claude.Enabled, path: cfg.Claude.Path, resolved: &resolved.Claude},
		{name: "pi", enabled: cfg.Pi.Enabled, path: cfg.Pi.Path, resolved: &resolved.Pi},
		{name: "codex", enabled: cfg.Codex.Enabled, path: cfg.Codex.Path, resolved: &resolved.Codex},
		{name: "agents", enabled: cfg.Agents.Enabled, path: cfg.Agents.Path, resolved: &resolved.Agents},
	}
	var enabled []resolvedTarget
	for _, target := range configured {
		path, err := resolveManagedPath("targets."+target.name+".path", target.path, home)
		if err != nil {
			return ResolvedTargets{}, nil, err
		}
		*target.resolved = path
		if !target.enabled {
			continue
		}
		expanded, err := expandHome(target.path, home)
		if err != nil {
			return ResolvedTargets{}, nil, err
		}
		if info, statErr := os.Lstat(filepath.Clean(expanded)); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return ResolvedTargets{}, nil, fmt.Errorf("config: targets.%s.path must not be a symlink", target.name)
		}
		if path == string(filepath.Separator) || samePath(path, home) {
			return ResolvedTargets{}, nil, fmt.Errorf("config: targets.%s.path is too broad", target.name)
		}
		for _, prior := range enabled {
			if pathsOverlap(prior.path, path) {
				return ResolvedTargets{}, nil, fmt.Errorf("config: enabled target %q overlaps enabled target %q", target.name, prior.name)
			}
		}
		enabled = append(enabled, resolvedTarget{name: target.name, path: path})
	}
	return resolved, enabled, nil
}

// resolveSessions resolves the read-only session transcript roots. Session
// roots are harness-owned inputs, so unlike targets they need no symlink,
// breadth, or overlap restrictions.
func resolveSessions(cfg Sessions, home string) (ResolvedSessions, error) {
	resolved := ResolvedSessions{}
	configured := []struct {
		name     string
		path     string
		resolved *string
	}{
		{name: "claude", path: cfg.Claude.Path, resolved: &resolved.Claude},
		{name: "pi", path: cfg.Pi.Path, resolved: &resolved.Pi},
		{name: "codex", path: cfg.Codex.Path, resolved: &resolved.Codex},
	}
	for _, root := range configured {
		path, err := resolveManagedPath("sessions."+root.name+".path", root.path, home)
		if err != nil {
			return ResolvedSessions{}, err
		}
		*root.resolved = path
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

func pathsOverlap(left, right string) bool {
	return pathContains(left, right) || pathContains(right, left)
}

func pathContains(parent, child string) bool {
	parent = normalizePath(parent)
	child = normalizePath(child)
	rel, err := filepath.Rel(parent, child)
	return err == nil && (rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func samePath(left, right string) bool {
	return normalizePath(left) == normalizePath(right)
}

func normalizePath(path string) string {
	return naming.PathKey(filepath.Clean(path))
}

func sanitizeCommentValue(value string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(value)
}
