package ui

// Shared help fragments compose command long descriptions so repeated
// contract text cannot drift between commands. Fragments carry no leading or
// trailing newline; compose them with explicit separators.

// StreamContractHelp describes output streams and terminal styling.
const StreamContractHelp = `Payload goes to stdout; human-readable diagnostics and final error messages
go to stderr. Human tables use color only on terminals and honor NO_COLOR;
redirected output contains no terminal escapes.`

// JSONContractHelp describes structured output and failure reporting.
const JSONContractHelp = `--json emits one complete JSON document, including structured diagnostics,
to stdout and does not duplicate an unsuccessful report as a stderr error;
the exit status still reports failure.`
