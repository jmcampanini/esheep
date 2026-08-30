package cmd

import (
	"context"
	"errors"

	"github.com/jmcampanini/esheep/internal/config"
	"github.com/jmcampanini/esheep/internal/manage"
	"github.com/jmcampanini/esheep/internal/ui"
	"github.com/spf13/cobra"
)

func newSkillsCommand(load configLoader, operations commandOperations) *cobra.Command {
	command := &cobra.Command{
		Use:   "skills",
		Short: "Inspect known skills and deployment status",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	command.AddCommand(
		newSkillsListCommand(load, operations.list),
		newSkillsStatusCommand(load, operations.status),
	)
	return command
}

func newSkillsListCommand(load configLoader, list func(context.Context, config.LoadResult) manage.ListReport) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "list",
		Short: "List skills known from configured sources",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			loaded, err := loadConfiguration(command, load)
			if err != nil {
				return err
			}
			report := list(command.Context(), loaded)
			if jsonOutput {
				if err := ui.WriteListJSON(command.OutOrStdout(), report); err != nil {
					return appError(err)
				}
				if !report.Complete {
					return silentAppError(errors.New("skills inventory is incomplete"))
				}
				return nil
			}
			if err := ui.WriteList(command.OutOrStdout(), report, ui.ShouldColor(command.OutOrStdout())); err != nil {
				return appError(err)
			}
			if err := ui.WriteDiagnostics(command.ErrOrStderr(), report.Diagnostics); err != nil {
				return appError(err)
			}
			if !report.Complete {
				return appError(errors.New("skills inventory is incomplete"))
			}
			return nil
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "emit one JSON document")
	return command
}

func newSkillsStatusCommand(load configLoader, status func(context.Context, config.LoadResult) manage.StatusReport) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "status",
		Short: "Report per-target deployment health",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			loaded, err := loadConfiguration(command, load)
			if err != nil {
				return err
			}
			report := status(command.Context(), loaded)
			if jsonOutput {
				if err := ui.WriteStatusJSON(command.OutOrStdout(), report); err != nil {
					return appError(err)
				}
				if !report.Healthy {
					return silentAppError(errors.New("skills status is unhealthy"))
				}
				return nil
			}
			if err := ui.WriteStatus(command.OutOrStdout(), report, ui.ShouldColor(command.OutOrStdout())); err != nil {
				return appError(err)
			}
			if err := ui.WriteDiagnostics(command.ErrOrStderr(), report.Diagnostics); err != nil {
				return appError(err)
			}
			if !report.Healthy {
				return appError(errors.New("skills status is unhealthy"))
			}
			return nil
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "emit one JSON document")
	return command
}
