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

'memory record' appends one entry to a file named by the calling harness
session inside a directory named by the project the session works in. The
root defaults to $XDG_DATA_HOME/esheep/memory, or
~/.local/share/esheep/memory, and is configured by [memory].path in TOML,
ESHEEP_MEMORY_PATH, or --memory-path. esheep only ever appends under that
root; reading and reviewing recorded memories is not part of this command.`,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	command.AddCommand(newMemoryRecordCommand(load, record))
	return command
}
