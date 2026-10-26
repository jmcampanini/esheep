package ui

// Shared help fragments compose command long descriptions so repeated
// contract text cannot drift between commands. Fragments carry no leading or
// trailing newline; compose them with explicit separators.

// StreamContractHelp describes output streams and terminal styling.
const StreamContractHelp = `Payload goes to stdout; human-readable diagnostics and final error messages
go to stderr. Human tables carry color only under --color=auto (the
default) when the receiving stream is a terminal, NO_COLOR is unset or
empty, and TERM is not dumb; --color=always and --color=never override
that detection. Each stream is judged on its own, and --json output never
contains terminal escapes.`

// JSONContractHelp describes structured output and failure reporting.
const JSONContractHelp = `--json emits one complete JSON document, including structured diagnostics,
to stdout and does not duplicate an unsuccessful report as a stderr error;
the exit status still reports failure.`
