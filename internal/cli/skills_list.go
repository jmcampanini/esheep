package cli

import (
	"context"
	"errors"

	"github.com/jmcampanini/esheep/internal/apperr"
	"github.com/jmcampanini/esheep/internal/config"
	"github.com/jmcampanini/esheep/internal/manage"
	"github.com/jmcampanini/esheep/internal/ui"
	"github.com/spf13/cobra"
)

func newSkillsListCommand(load configLoader, list func(context.Context, config.LoadResult) manage.ListReport) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "list",
		Short: "List skills known from configured sources",
		Long: `Inventory every skill discovered in configured sources without changing
sources or targets.

Readiness is ready, disabled, invalid, collision, or conflict. Disabled
means the selected manifest declares esheep-disabled: true; validation
errors, collisions, and profile conflicts take precedence. Diagnostics
do not hide known entries. The profile gate column shows when a manifest
applies: all means every profile, and - means no manifest was loadable.
The command exits nonzero only when configuration or filesystem failures
prevent complete discovery.

` + ui.StreamContractHelp + `

` + ui.JSONContractHelp + ` List JSON includes "complete" and
"effective_profiles". Each skill's optional "trigger" contains its invocation
text from esheep-trigger. Its optional "profile_gate" lists the profiles
that limit manifest selection.`,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			loaded, err := loadConfiguration(command, load)
			if err != nil {
				return err
			}
			report := list(command.Context(), loaded)
			if jsonOutput {
				if err := ui.WriteListJSON(command.OutOrStdout(), report); err != nil {
					return apperr.Wrap(err)
				}
				if !report.Complete {
					return apperr.WrapSilent(errors.New("skills inventory is incomplete"))
				}
				return nil
			}
			if err := ui.WriteList(command.OutOrStdout(), report, ui.ShouldColor(command.OutOrStdout())); err != nil {
				return apperr.Wrap(err)
			}
			if err := ui.WriteDiagnostics(command.ErrOrStderr(), report.Diagnostics); err != nil {
				return apperr.Wrap(err)
			}
			if !report.Complete {
				return apperr.Wrap(errors.New("skills inventory is incomplete"))
			}
			return nil
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "emit one JSON document")
	return command
}
