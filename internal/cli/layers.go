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

func newLayersCommand(load configLoader, layers func(context.Context, config.LoadResult) manage.LayersReport) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "layers",
		Short: "Report effective and referenced layers",
		Long: `Report the effective layer list and every layer referenced by skills and
agents files discovered in configured sources.

Effective is the resolved layer list: the layers setting from flags,
ESHEEP_LAYERS, or the TOML file, appended with the comma-separated values
of every environment variable named by env_layers, then deduplicated by
keeping the last occurrence of each name in order. The combined names
work, laptop, work give the effective layers laptop, work. The automatic
base and local layers are never effective: SKILL.md and AGENTS.md are the
base layer and SKILL.local.md and AGENTS.local.md are the local layer, and
both apply whenever present, subject to any esheep-only-layers gate on a
skill manifest. Referenced is the sorted union of valid layer
names found in SKILL.<layer>.md and AGENTS.<layer>.md filenames,
esheep-only-layers, and esheep-targets gates; local is never referenced.
Invalid names remain diagnostics but are excluded from Referenced. The
command exits nonzero only when configuration or filesystem failures
prevent complete discovery.

` + ui.StreamContractHelp + `

` + ui.JSONContractHelp + ` Layers JSON includes "complete".`,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			loaded, err := loadConfiguration(command, load)
			if err != nil {
				return err
			}
			report := layers(command.Context(), loaded)
			if jsonOutput {
				if err := ui.WriteLayersJSON(command.OutOrStdout(), report); err != nil {
					return apperr.Wrap(err)
				}
			} else if err := ui.WriteLayers(command.OutOrStdout(), report); err != nil {
				return apperr.Wrap(err)
			}
			if err := ui.WriteDiagnostics(command.ErrOrStderr(), report.Diagnostics); err != nil {
				return apperr.Wrap(err)
			}
			if !report.Complete {
				return apperr.Wrap(errors.New("layers inventory is incomplete"))
			}
			return nil
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "emit one JSON document")
	return command
}
