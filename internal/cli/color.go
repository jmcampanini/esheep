package cli

import (
	"io"

	"github.com/jmcampanini/esheep/internal/config"
	"github.com/jmcampanini/esheep/internal/ui"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const colorFlag = "color"

// registerColorFlag adds the --color flag. It is presentation, not
// configuration, so it is registered here rather than through the
// configuration package.
func registerColorFlag(flags *pflag.FlagSet) {
	mode := ui.ColorMode(config.DefaultColor)
	flags.Var(&mode, colorFlag, "color human output: auto, always, or never")
}

// useColor reports whether the command's human output to writer should
// carry terminal styling under the --color mode given to the root.
func useColor(command *cobra.Command, writer io.Writer) bool {
	flag := command.Root().PersistentFlags().Lookup(colorFlag)
	if flag == nil {
		panic("color flag is not registered on the root command")
	}
	mode, ok := flag.Value.(*ui.ColorMode)
	if !ok {
		panic("color flag does not hold a color mode")
	}
	return ui.UseColor(*mode, writer)
}
