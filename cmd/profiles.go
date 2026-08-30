package cmd

import (
	"context"
	"errors"

	"github.com/jmcampanini/esheep/internal/config"
	"github.com/jmcampanini/esheep/internal/manage"
	"github.com/jmcampanini/esheep/internal/ui"
	"github.com/spf13/cobra"
)

func newProfilesCommand(load configLoader, profiles func(context.Context, config.LoadResult) manage.ProfilesReport) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "profiles",
		Short: "Report effective and referenced profiles",
		Long: `Report the effective profile list and every profile referenced by skills
discovered in configured sources.

Effective is the resolved active profile list: the profiles setting from
flags, ESHEEP_PROFILES, or the TOML file, appended with the comma-separated
values of every environment variable named by env_profiles, deduplicated in
first-seen order. Referenced is the sorted union of valid profile names that
manifest filenames and esheep-only-profiles fields gate on; invalid gates
remain diagnostics but are excluded from Referenced. The command exits
nonzero only when configuration or filesystem failures prevent complete
discovery.

` + streamContractHelp + `

` + jsonContractHelp + ` Profiles JSON includes "complete".`,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			loaded, err := loadConfiguration(command, load)
			if err != nil {
				return err
			}
			report := profiles(command.Context(), loaded)
			if jsonOutput {
				if err := ui.WriteProfilesJSON(command.OutOrStdout(), report); err != nil {
					return appError(err)
				}
				if !report.Complete {
					return silentAppError(errors.New("profiles inventory is incomplete"))
				}
				return nil
			}
			if err := ui.WriteProfiles(command.OutOrStdout(), report); err != nil {
				return appError(err)
			}
			if err := ui.WriteDiagnostics(command.ErrOrStderr(), report.Diagnostics); err != nil {
				return appError(err)
			}
			if !report.Complete {
				return appError(errors.New("profiles inventory is incomplete"))
			}
			return nil
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "emit one JSON document")
	return command
}
