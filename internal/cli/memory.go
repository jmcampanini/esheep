package cli

import (
	"context"

	"github.com/jmcampanini/esheep/internal/memory"
	"github.com/spf13/cobra"
)

func newMemoryCommand(load configLoader, record func(context.Context, memory.Request) (memory.Result, error)) *cobra.Command {
	command := &cobra.Command{
		Use:   "memory",
		Short: "Record memories from harness sessions",
		Long: `Record memories that coding agents want to keep beyond one session.

Memories live under the memory root, in a file named by the calling
harness session inside a directory named by the project the session
works in. Run 'esheep config' to see the resolved memory root and 'esheep
config --help' for path settings and their precedence. The record command
creates the root and its missing parents when needed, creates and appends
files beneath it, and touches nothing else; reading and reviewing recorded
memories is not part of this group.

A bare 'esheep memory' prints this help on stdout. The member command
describes its own stdout and stderr; none offers a machine-readable form
or prompts.`,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	command.AddCommand(newMemoryRecordCommand(load, record))
	return command
}
