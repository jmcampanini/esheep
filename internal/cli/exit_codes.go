package cli

import "github.com/spf13/cobra"

func newExitCodesTopic() *cobra.Command {
	return newHelpTopic("exit-codes", "Exit statuses and the failure format", `esheep uses a three-value exit scheme:

  0  Success.
  1  Application failure: a value rejected by the command after parsing,
     invalid configuration, transcript input, or output prevented the
     command from succeeding. 'sessions list' and 'sessions search' exit 1
     when the scan is incomplete.
  2  Usage error: an unknown command, flag, or operand; operands that
     fail a command's positional grammar; or a flag value the command
     library cannot parse.

A failure prints exactly one line on stderr in the form

  esheep: <message>

and no usage text, for usage errors as well. Diagnostics a command prints
while it works precede that line in every output mode. An incomplete
report keeps its payload on stdout and still ends with that
line; a --json document carries results only, never diagnostics or error
text.

These conditions keep status 0 because the command's own work completed.
Every output mode reports them as diagnostics on stderr.

  'sessions list' and 'sessions search' report a missing or unconfigured
  session root, malformed transcript lines, undated events, and invalid or
  mismatched Cowork companion metadata while the scan is complete.`)
}
