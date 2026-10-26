package cli

import "github.com/spf13/cobra"

func newExitCodesTopic() *cobra.Command {
	return newHelpTopic("exit-codes", "Exit statuses and the failure format", `esheep uses a three-value exit scheme:

  0  Success.
  1  Application failure: configuration, source input, target state, or
     output prevented a valid command from succeeding. 'sync' exits 1 when
     its final summary counts failures, including blocked actions, after
     unrelated work is attempted. 'skills status' exits 1 whenever
     deployment health is not proven. 'skills list' and 'layers' exit 1
     only when discovery is incomplete. 'sessions list', 'sessions
     search', and 'sessions query' exit 1 only when the scan is
     incomplete, including when a selected machine cannot answer.
     'doctor' exits 1 when any check fails. 'memory record' exits 1 when
     no session can be derived or the project lookup or append fails.
  2  Usage error: an unknown command, flag, or operand; operands that
     fail a command's grammar; a flag or operand value outside its fixed
     set; or a command wiring failure.

A failure prints exactly one line on stderr in the form

  esheep: <message>

and no usage text, for usage errors as well. Diagnostics a command prints
while it works precede that line. JSON modes report an unsuccessful
result inside the emitted document and still use the exit status; they
do not duplicate the report as a stderr error.

These failures are reported on stderr but keep status 0 because the
command's own work completed:

  'esheep help <topic>' with an unknown topic prints 'Unknown help topic'
  and the root usage on stderr.
  'skills list' and 'layers' print diagnostics for invalid or colliding
  source skills while discovery is complete.
  'sessions list' and 'sessions search' print diagnostics for a missing
  or unconfigured session root, malformed transcript lines, undated
  events, and '--remote all' with no machine configured while the scan is
  complete.
  'doctor' reports a skipped check.
  'memory record' reports a missing git binary or a local-path origin and
  still records the memory under the fallback project.`)
}
