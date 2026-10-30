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
by comments showing the resolved session paths.
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
session defaults;
$XDG_CONFIG_HOME/esheep/config.toml, or $HOME/.config/esheep/config.toml
when XDG_CONFIG_HOME is unset, empty, or not an absolute path;
CODEX_HOME for the Codex session home when it is non-empty;
ESHEEP_CLAUDE_SESSIONS_PATH, ESHEEP_CLAUDE_COWORK_SESSIONS_PATH,
ESHEEP_PI_SESSIONS_PATH, and ESHEEP_CODEX_HOME variables; then the
session flags. Paths are derived and validated after all overrides.
--config PATH replaces automatic discovery and requires a loadable file,
including when PATH is empty.`
