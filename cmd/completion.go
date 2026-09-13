package cmd

import "github.com/spf13/cobra"

func newCompletionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate a shell completion script",
		Long: `Write a completion script for the named shell to stdout.

For example, 'esheep completion zsh' writes Zsh completion. Load the
script as your shell's documentation describes. Completion does not load
configuration.`,
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		RunE: func(command *cobra.Command, args []string) error {
			root := command.Root()
			var err error
			switch args[0] {
			case "bash":
				err = root.GenBashCompletion(command.OutOrStdout())
			case "zsh":
				err = root.GenZshCompletion(command.OutOrStdout())
			case "fish":
				err = root.GenFishCompletion(command.OutOrStdout(), true)
			case "powershell":
				err = root.GenPowerShellCompletion(command.OutOrStdout())
			}
			return appError(err)
		},
	}
}
