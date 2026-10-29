package cli

import (
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
~/.pi/agent/sessions, and ~/.codex and must not
be empty. [sessions.claude-cowork].path locates local Claude Cowork
conversations; it defaults to the platform location on macOS, has no
default on Linux, and an empty value disables Cowork discovery. 'esheep
sessions --help' describes what is read under each root.

[memory].path is the root 'memory record' appends under. It defaults to
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
			return config.WriteReport(
				command.OutOrStdout(),
				loaded,
				config.ReportOptions{Provenance: provenance},
			)
		},
	}
	command.Flags().BoolVar(&provenance, "provenance", false, "include field-level source information")
	return command
}

const configResolutionHelp = `Settings load in this order, with later sources taking precedence: built-in
session and memory defaults;
$XDG_CONFIG_HOME/esheep/config.toml, or $HOME/.config/esheep/config.toml
when XDG_CONFIG_HOME is unset, empty, or not an absolute path;
CODEX_HOME for the Codex session home and $XDG_DATA_HOME/esheep/memory
for the memory root when those variables are non-empty;
ESHEEP_CLAUDE_SESSIONS_PATH, ESHEEP_CLAUDE_COWORK_SESSIONS_PATH,
ESHEEP_PI_SESSIONS_PATH, ESHEEP_CODEX_HOME, and ESHEEP_MEMORY_PATH
variables; then the session and memory flags. Paths are derived and
validated after all overrides. XDG_DATA_HOME must be absolute when it
supplies the effective memory root. --config PATH replaces automatic
discovery and requires a loadable file, including when PATH is empty.
Machines are configured only in the TOML file.`
