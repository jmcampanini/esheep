package cli

import (
	"github.com/jmcampanini/esheep/internal/apperr"
	"github.com/jmcampanini/esheep/internal/config"
	"github.com/spf13/cobra"
)

func newConfigCommand(load configLoader) *cobra.Command {
	var provenance bool
	command := &cobra.Command{
		Use:   "config",
		Short: "Print the effective configuration",
		Long: `Print the effective configuration as redirectable TOML on stdout, followed
by comments showing resolved configuration, source, and target paths.
--provenance adds the source of each setting. The output includes every
effective setting, including defaults, so it contains more than you need
to put in config.toml.

The command reads but never creates or modifies config.toml, works before
any source directory exists, and fails before any skill or target
processing when configuration is invalid. Failures go to stderr as one
line; the command never prompts and has no other output format.

Every path setting must be absolute, exactly '~', or begin with '~/';
symlinks in it are resolved. The keys config.toml accepts:

layers is a list of layer names. A name is 1-64 characters of lowercase
alphanumeric words separated by single hyphens; base and local are
reserved.

env_layers is a list of environment variable names, each nonblank with no
surrounding whitespace; blank entries in a named variable are skipped.

[[sources]] entries name the read-only skill containers. name identifies
the source in reports: 1-255 characters in '/'-separated segments, each
starting with a letter or digit and continuing with letters, digits,
'.', '_', and '-', with no leading or trailing '/' and no '.' or '..'
segment; names must be unique ignoring case. path is the container root.
Source roots must be distinct, non-nested, and must not overlap an
enabled target's skills_path or agents_md_path. 'esheep help skill-format'
describes the container layout. At least one source is needed before
'sync' has anything to install.

[targets.claude], [targets.pi], and [targets.codex] are each optional
with keys enabled, skills_path, and agents_md_path. All three targets are
enabled by default; omitted settings keep these defaults:

  Target  skills_path           agents_md_path
  claude  ~/.claude/skills      ~/.claude/CLAUDE.md
  pi      ~/.pi/agent/skills    ~/.pi/agent/AGENTS.md
  codex   ~/.agents/skills      ~/.codex/AGENTS.md

Enabled target skills paths must be distinct and non-nested, may not be
symlinks, and may not be '/' or the home directory. An enabled
agents_md_path must be a non-symlink file path, distinct from every other
enabled agents_md_path, outside every source root and enabled skills_path,
and may not be the settings file, an existing non-regular file, '/', or
the home directory. Disabled targets are validated as paths only.

[sessions.claude].path, [sessions.pi].path, and [sessions.codex].home
locate the harness-owned transcript roots the sessions commands read in
place; Codex transcripts are the sessions and archived_sessions
directories under the home. Each defaults to ~/.claude/projects,
~/.pi/agent/sessions, and ~/.codex (or CODEX_HOME when set) and must not
be empty. [sessions.claude-cowork].path locates local Claude Cowork
conversations; it defaults to the platform location on macOS, has no
default on Linux, and an empty value disables Cowork discovery. Session
roots carry no overlap restrictions. 'esheep sessions --help' describes
what is read under each root.

[memory].path is the root 'memory record' appends under. It defaults to
$XDG_DATA_HOME/esheep/memory when XDG_DATA_HOME is set, otherwise
~/.local/share/esheep/memory. It may not overlap a source root or an
enabled target skills path, and may not be '/' or the home directory.

[[machines]] entries name other machines that run esheep, for
'sessions list --remote' and 'sessions search --remote'. They are
configured only in the TOML file:

  [[machines]]
  name = "nas"                   # hostname; ssh destination defaults to it

  [[machines]]
  name = "studio"
  host = "javier@studio.ts.net"  # ssh destination (default: name)
  command = "/opt/bin/esheep"    # remote command (default: esheep)
  timeout = "2m"                 # whole call, Go duration (default: 60s)

An entry has exactly those four keys; transcript locations are the remote
machine's own configuration. name is the machine's hostname: the entry
whose first label matches this machine's is this machine. Names must be
non-empty, unique ignoring case, and free of commas and whitespace; 'all'
is reserved. command is inserted verbatim into the ssh command line, so it
can name a path the remote login shell lacks on PATH. timeout must be a
positive Go duration. The output lists every entry with its effective
host, command, and timeout.

Any other key, in any table, is an error naming the key.

` + configResolutionHelp,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			loaded, err := loadConfiguration(command, load)
			if err != nil {
				return err
			}
			return apperr.Wrap(config.WriteReport(
				command.OutOrStdout(),
				loaded,
				config.ReportOptions{Provenance: provenance, Redact: redactConfig},
			))
		},
	}
	command.Flags().BoolVar(&provenance, "provenance", false, "include field-level source information")
	return command
}

func redactConfig(loaded config.LoadResult) config.LoadResult {
	return loaded
}

const configResolutionHelp = `Settings load in this order, with later sources taking precedence: built-in
target, session, and memory defaults (CODEX_HOME supplies the Codex session
home default and XDG_DATA_HOME the memory root default when set);
$XDG_CONFIG_HOME/esheep/config.toml, or $HOME/.config/esheep/config.toml
when XDG_CONFIG_HOME is unset, empty, or not an absolute path;
ESHEEP_LAYERS, ESHEEP_<TARGET>_ENABLED, ESHEEP_<TARGET>_SKILLS_PATH,
ESHEEP_<TARGET>_AGENTS_MD_PATH, ESHEEP_CLAUDE_SESSIONS_PATH,
ESHEEP_CLAUDE_COWORK_SESSIONS_PATH, ESHEEP_PI_SESSIONS_PATH,
ESHEEP_CODEX_HOME, and ESHEEP_MEMORY_PATH variables; then the --layers,
--layer, target, session, and memory flags. A higher-precedence layers
value replaces the lower one rather than appending to it, and an empty
value such as --layers "" clears it. --config PATH replaces automatic
discovery and requires a loadable file. Source directories and env_layers
are configured only in the TOML file; every environment variable named by
env_layers appends its comma-separated layers after the layers setting,
whatever supplied or cleared it. 'esheep layers --help' describes
deduplication and the automatic base and local layers.`
