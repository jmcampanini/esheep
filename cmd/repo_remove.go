package cmd

import (
	"fmt"

	"github.com/jmcampanini/esheep/internal/registry"
	"github.com/spf13/cobra"
)

func newRepoRemoveCommand(load configLoader) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a skill repository",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if err := registry.ValidateName(args[0]); err != nil {
				return appError(err)
			}
			loaded, err := loadConfiguration(command, load)
			if err != nil {
				return err
			}
			if err := registry.Remove(
				loaded.Locations.Registry,
				args[0],
				loaded.Locations.CloneRoot,
			); err != nil {
				return appError(err)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Removed %s\n", args[0])
			return appError(err)
		},
	}
}
