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
works in. The root defaults to $XDG_DATA_HOME/esheep/memory, or
~/.local/share/esheep/memory, and is configured by [memory].path in TOML,
ESHEEP_MEMORY_PATH, or --memory-path. This group creates the root and its
missing parents when needed, creates and appends files beneath it, and
touches nothing else; reading and reviewing recorded memories is not part
of it.

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
