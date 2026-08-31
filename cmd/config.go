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

` + configResolutionHelp + `

Source and target paths must be absolute, exactly '~', or begin with '~/'.
Source roots must be distinct and non-nested. Enabled target skills paths
must also be distinct and non-nested, may not be symlinks, may not overlap
a source root, and may not be '/' or the home directory. Enabled target
agents_md_path values must be distinct, non-symlink file paths outside every
source root and enabled skills path, and may not be the settings file, an
existing directory, '/', or the home directory.

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
