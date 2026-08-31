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
		Long: `Inspect skills and their deployment status without changing sources or
targets.

'skills list' inventories configured sources; 'skills status' checks
per-target deployment health.`,
		Args: cobra.NoArgs,
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
		Long: `Inventory every skill discovered in configured sources without changing
sources or targets.

Readiness is ready, invalid, collision, or conflict; validation and
collision diagnostics do not hide known entries. The profile gate column
shows when a manifest applies: all means every profile, and - means no manifest
was loadable. The command exits nonzero only when configuration or
filesystem failures prevent complete discovery.

` + streamContractHelp + `

` + jsonContractHelp + ` List JSON includes "complete" and
"effective_profiles". Each skill's optional "profile_gate" lists the profiles
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
		Long: `Report source readiness and per-target deployment health under the
effective profiles.

Each ready skill is synced, drifted, missing, inactive, disabled, or
blocked for every target; blocked means a destination or target cannot be
inspected or managed safely, inactive means no manifest applies under the
active profiles, and disabled means target configuration or esheep-targets
excludes installation. Every enabled target is inspected even when no skills
are discovered; missing and valid empty targets remain healthy.

When a source provides an agents file, status adds a section reporting the
selected file and each target's deployed copy compared byte-for-byte:
synced, stale, missing, disabled, or blocked. A withdrawn agents file is
not reported; its stray deployed copies are invisible to esheep.

Status is a health check: it exits 0 only when every source skill is
ready, every target is synced, inactive, or disabled, and every enabled
target's agents file is synced.

` + streamContractHelp + `

` + jsonContractHelp + ` Status JSON includes "healthy",
"effective_profiles", and an optional "agents_file" section naming the
selected source file and per-target states. Each skill's optional
"profile_gate" lists the profiles that limit manifest selection.`,
		Args: cobra.NoArgs,
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
