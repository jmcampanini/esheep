package cli

import (
	"github.com/jmcampanini/esheep/internal/apperr"
	"github.com/spf13/cobra"
)

// wrapCommandRunners marks every runner failure as an application error.
// Command discovery, flag parsing, and Args validation remain usage errors.
func wrapCommandRunners(command *cobra.Command) {
	if run := command.RunE; run != nil {
		command.RunE = func(command *cobra.Command, args []string) error {
			return apperr.Wrap(run(command, args))
		}
	}
	for _, child := range command.Commands() {
		wrapCommandRunners(child)
	}
}
