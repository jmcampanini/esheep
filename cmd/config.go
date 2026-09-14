package cmd

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
by comments showing resolved configuration, source, and target paths.
--provenance adds the source of each setting.

Claude, Pi, and Codex are enabled by default. Target sections are optional.
Set enabled = false to disable a target, or set skills_path or agents_md_path
to override a path. Omitted settings retain these defaults:

  Target  skills_path           agents_md_path
  claude  ~/.claude/skills      ~/.claude/CLAUDE.md
  pi      ~/.pi/agent/skills    ~/.pi/agent/AGENTS.md
  codex   ~/.agents/skills      ~/.codex/AGENTS.md

The output includes all effective settings, including defaults, so it
contains more than you need to put in esheep.toml.

[[machines]] entries name other machines that run esheep, for
'sessions list --remote' and 'sessions search --remote'. They are
configured only in the TOML file:

  [[machines]]
  name = "nas"                           # hostname; ssh destination defaults to it

  [[machines]]
  name = "studio"
  host = "javier@studio.tail1234.ts.net" # ssh destination (default: name)
  command = "/opt/homebrew/bin/esheep"   # remote command (default: esheep)
  timeout = "2m"                         # whole call, Go duration (default: 60s)

An entry has exactly those four keys; transcript locations are the remote
machine's own configuration. name is the machine's hostname: the entry
whose first label matches this machine's is this machine. Names must be
non-empty, unique ignoring case, and free of commas and whitespace; 'all'
is reserved. command is inserted verbatim into the ssh command line, so it
can name a path the remote login shell lacks on PATH. timeout must be a
positive Go duration. The output lists every entry with its effective
host, command, and timeout.

` + configResolutionHelp + `

Source and target paths must be absolute, exactly '~', or begin with '~/'.
Source roots must be distinct and non-nested. Enabled target skills paths
must also be distinct and non-nested, may not be symlinks, may not overlap
a source root, and may not be '/' or the home directory. Enabled target
agents_md_path values must be distinct, non-symlink file paths outside every
source root and enabled skills path, and may not be the settings file, an
existing non-regular file, '/', or the home directory.

The command reads but never creates or modifies esheep.toml, works before
any source directory exists, and fails before any skill or target
processing when configuration is invalid.`,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			loaded, err := loadConfiguration(command, load)
			if err != nil {
				return err
			}
			return appError(config.WriteReport(
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
