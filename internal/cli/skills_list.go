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

Readiness is ready, disabled, invalid, or collision. Disabled means the
selected manifest declares esheep-disabled: true; validation errors and
collisions take precedence. Diagnostics do not hide known entries. The
layer gate column lists every layer name a manifest's eligibility depends
on, from SKILL.<layer>.md filenames and esheep-only-layers gates; a gated
variant needs its filename layer and a gate name together. all means some
manifest applies under any effective layers, and - means no manifest was
loadable.
The command exits nonzero only when configuration or filesystem failures
prevent complete discovery.

` + ui.StreamContractHelp + `

` + ui.JSONContractHelp + ` List JSON includes "complete" and
"effective_layers". Each skill's optional "trigger" contains its invocation
text from esheep-trigger. Its optional "layer_gate" lists the layer names
its manifests' eligibility depends on.`,
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
