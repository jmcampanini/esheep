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
by comments showing the resolved session, memory, and machine settings.
--provenance adds the source of each setting. The output includes every
effective setting, including defaults, so it contains more than you need
to put in config.toml.

The command reads but never creates or modifies config.toml and fails
before any other work when configuration is invalid. Failures go to
stderr as one line; the command never prompts and has no other output
format.

Every path setting must be absolute, exactly '~', or begin with '~/';
symlinks in it are resolved. The keys config.toml accepts:

[sessions.claude].path, [sessions.pi].path, and [sessions.codex].home
locate the harness-owned transcript roots the sessions commands read in
place; Codex transcripts are the sessions and archived_sessions
directories under the home. Each defaults to ~/.claude/projects,
~/.pi/agent/sessions, and ~/.codex (or CODEX_HOME when set) and must not
be empty. [sessions.claude-cowork].path locates local Claude Cowork
conversations; it defaults to the platform location on macOS, has no
default on Linux, and an empty value disables Cowork discovery. 'esheep
sessions --help' describes what is read under each root.

[memory].path is the root 'memory record' appends under. It defaults to
$XDG_DATA_HOME/esheep/memory when XDG_DATA_HOME is set, otherwise
~/.local/share/esheep/memory. It may not be '/' or the home directory.

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
session and memory defaults (CODEX_HOME supplies the Codex session home
default and XDG_DATA_HOME the memory root default when set);
$XDG_CONFIG_HOME/esheep/config.toml, or $HOME/.config/esheep/config.toml
when XDG_CONFIG_HOME is unset, empty, or not an absolute path;
ESHEEP_CLAUDE_SESSIONS_PATH, ESHEEP_CLAUDE_COWORK_SESSIONS_PATH,
ESHEEP_PI_SESSIONS_PATH, ESHEEP_CODEX_HOME, and ESHEEP_MEMORY_PATH
variables; then the session and memory flags. --config PATH replaces
automatic discovery and requires a loadable file. Machines are configured
only in the TOML file.`
