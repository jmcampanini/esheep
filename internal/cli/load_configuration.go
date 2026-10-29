package cli

import (
	"github.com/jmcampanini/esheep/internal/config"
	"github.com/spf13/cobra"
)

func loadConfiguration(command *cobra.Command, load configLoader) (config.LoadResult, error) {
	loaded, err := load(config.LoadOptions{Flags: command.Root().PersistentFlags()})
	if err != nil {
		return config.LoadResult{}, err
	}
	return loaded, nil
}
