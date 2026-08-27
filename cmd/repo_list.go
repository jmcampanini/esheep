package cmd

import (
	"fmt"
	"text/tabwriter"

	"github.com/jmcampanini/esheep/internal/registry"
	"github.com/spf13/cobra"
)

func newRepoListCommand(load configLoader) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List registered skill repositories",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			loaded, err := loadConfiguration(command, load)
			if err != nil {
				return err
			}
			repositories, err := registry.List(loaded.Locations.Registry)
			if err != nil {
				return appError(err)
			}
			writer := tabwriter.NewWriter(command.OutOrStdout(), 0, 8, 2, ' ', 0)
			if _, err := fmt.Fprintln(writer, "NAME\tURL"); err != nil {
				return appError(err)
			}
			for _, repository := range repositories {
				if _, err := fmt.Fprintf(writer, "%s\t%s\n", repository.Name, repository.URL); err != nil {
					return appError(err)
				}
			}
			return appError(writer.Flush())
		},
	}
}
