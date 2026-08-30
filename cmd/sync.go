package cmd

import (
	"context"
	"fmt"

	"github.com/jmcampanini/esheep/internal/config"
	"github.com/jmcampanini/esheep/internal/manage"
	"github.com/jmcampanini/esheep/internal/ui"
	"github.com/spf13/cobra"
)

func newSyncCommand(load configLoader, synchronize func(context.Context, config.LoadResult) manage.SyncReport) *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Synchronize enabled skill targets",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			loaded, err := loadConfiguration(command, load)
			if err != nil {
				return err
			}
			report := synchronize(command.Context(), loaded)
			if err := ui.WriteSync(command.OutOrStdout(), report, ui.ShouldColor(command.OutOrStdout())); err != nil {
				return appError(err)
			}
			if err := ui.WriteDiagnostics(command.ErrOrStderr(), report.Diagnostics); err != nil {
				return appError(err)
			}
			if report.Summary.Failed != 0 {
				return appError(fmt.Errorf("sync completed with %d failure(s)", report.Summary.Failed))
			}
			return nil
		},
	}
}
