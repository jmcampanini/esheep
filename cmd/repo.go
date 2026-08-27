package cmd

import (
	"github.com/spf13/cobra"
)

func newRepoCommand(load configLoader) *cobra.Command {
	command := &cobra.Command{
		Use:   "repo",
		Short: "Manage skill repositories",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	command.AddCommand(
		newRepoAddCommand(load),
		newRepoListCommand(load),
		newRepoRemoveCommand(load),
	)
	return command
}
