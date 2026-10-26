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
     search', and 'sessions query' exit 1 when the scan is incomplete,
     including when a machine selected by --remote cannot answer, or
     when ssh or the local hostname cannot be found. 'doctor' exits 1
     when any check fails. 'memory record' exits 1 when no session can
     be derived or the project lookup or append fails.
  2  Usage error: an unknown command, flag, or operand; operands that
     fail a command's grammar; a flag or operand value outside its fixed
     set; or a command wiring failure.

A failure prints exactly one line on stderr in the form

  esheep: <message>

and no usage text, for usage errors as well. Diagnostics a command prints
while it works precede that line. JSON modes report an unsuccessful
result inside the emitted document and still use the exit status; they
do not duplicate the report as a stderr error.

These failures are reported in the output but keep status 0 because the
command's own work completed. Text modes print them as diagnostics on
stderr; --json carries them inside the document.

  'esheep help <topic>' with an unknown topic prints 'Unknown help topic'
  and the root usage on stderr.
  'skills list' and 'layers' report invalid or colliding source skills
  while discovery is complete.
  'sessions list' and 'sessions search' report a missing or unconfigured
  session root, malformed transcript lines, undated events, invalid or
  mismatched Cowork companion metadata, and '--remote all' with no
  machine configured while the scan is complete.
  'doctor' shows a skipped check as a row in its stdout table.
  'memory record' reports a missing git binary or a local-path origin on
  stderr and still records the memory under the fallback project.`)
}
