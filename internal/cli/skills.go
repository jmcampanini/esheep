package cli

import (
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
