package cmd

import (
	"fmt"

	"github.com/jmcampanini/esheep/internal/registry"
	"github.com/spf13/cobra"
)

func newRepoAddCommand(load configLoader) *cobra.Command {
	var name string
	command := &cobra.Command{
		Use:   "add <url>",
		Short: "Register a skill repository",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			parsed, err := registry.ParseSource(args[0])
			if err != nil {
				return appError(err)
			}
			effectiveName := parsed.Name
			if command.Flags().Changed("name") {
				if name == "" {
					return appError(fmt.Errorf("repository name must not be empty"))
				}
				effectiveName = name
			}
			if err := registry.ValidateName(effectiveName); err != nil {
				return appError(err)
			}
			loaded, err := loadConfiguration(command, load)
			if err != nil {
				return err
			}
			if command.Flags().Changed("name") {
				err = registry.Add(loaded.Locations.Registry, args[0], name)
			} else {
				err = registry.Add(loaded.Locations.Registry, args[0])
			}
			if err != nil {
				return appError(err)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Added %s\n", effectiveName)
			return appError(err)
		},
	}
	command.Flags().StringVar(&name, "name", "", "override the derived repository name")
	return command
}
