package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newHelpCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "help [command or topic]",
		Short: "Show help for a command or topic",
		Long: `Print help for the named command or topic to stdout, or root help when
no name is given. Nested commands use their full path, such as 'sessions
search'. An unknown name fails with one diagnostic line on stderr.

Help reads no files, changes nothing, never prompts, and works without an
interactive terminal. Its output is plain text with no machine-readable
format.`,
		Example:           "  esheep help sessions search\n  esheep help exit-codes",
		Args:              helpTopicArgs,
		ValidArgsFunction: completeHelp,
		RunE: func(command *cobra.Command, args []string) error {
			target, _, err := command.Root().Find(args)
			if err != nil {
				return err
			}
			target.SetContext(command.Context())
			target.InitDefaultHelpFlag()
			return target.Help()
		},
	}
}

func completeHelp(command *cobra.Command, args []string, prefix string) ([]cobra.Completion, cobra.ShellCompDirective) {
	target, remaining, err := command.Root().Find(args)
	if err != nil || len(remaining) != 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var completions []cobra.Completion
	for _, child := range target.Commands() {
		if (child.IsAvailableCommand() || child == command) && strings.HasPrefix(child.Name(), prefix) {
			completions = append(completions, cobra.CompletionWithDesc(child.Name(), child.Short))
		}
	}
	return completions, cobra.ShellCompDirectiveNoFileComp
}

// helpTopicArgs accepts only operands that name one command or help topic.
func helpTopicArgs(command *cobra.Command, args []string) error {
	if _, remaining, err := command.Root().Find(args); err != nil || len(remaining) != 0 {
		return fmt.Errorf("unknown help topic %q", strings.Join(args, " "))
	}
	return nil
}
