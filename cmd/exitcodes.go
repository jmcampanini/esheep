package cmd

import (
	"github.com/spf13/cobra"
)

func newExitCodesTopic() *cobra.Command {
	return &cobra.Command{
		Use:   "exit-codes",
		Short: "Exit codes and error categories",
		Long: `esheep uses a three-value exit scheme:

  0  Success.
  1  Application failure: configuration, source input, target state, or
     output prevented a valid command from succeeding. 'sync' exits 1 when
     its final report contains failures after unrelated work is attempted.
     'skills status' exits 1 whenever deployment health is not proven.
     'skills list' exits 1 only when discovery is incomplete.
  2  Invalid command usage or command wiring failure.

JSON modes report an unsuccessful result inside the emitted document and
still use the exit status; they do not duplicate the report as a stderr
error.`,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
}
