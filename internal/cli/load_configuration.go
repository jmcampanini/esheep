package cli

import (
	"github.com/jmcampanini/esheep/internal/config"
	"github.com/spf13/cobra"
)

func loadConfiguration(command *cobra.Command, load configLoader) (config.LoadResult, error) {
	return load(config.LoadOptions{Flags: command.Root().PersistentFlags()})
}
