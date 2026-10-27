package ui

// Shared help fragments compose command long descriptions so repeated
// contract text cannot drift between commands. Fragments carry no leading or
// trailing newline; compose them with explicit separators.

// StreamContractHelp describes the streams every command writes to and that
// no command prompts.
const StreamContractHelp = `Payload goes to stdout; human-readable diagnostics and the final failure
line go to stderr. The command never prompts and needs no terminal.`

// ColorContractHelp describes when a command that renders tables styles
// them; compose it only into commands that do.
const ColorContractHelp = `Tables carry color only under --color=auto (the default) when stdout is a
terminal, NO_COLOR is unset or empty, and TERM is not dumb; --color=always
and --color=never override that detection.`

// JSONContractHelp describes structured output and failure reporting.
const JSONContractHelp = `--json emits one complete JSON document to stdout. Diagnostics and the
final failure line go to stderr as in text mode, never into the document,
and the exit status still reports failure. The document never carries
terminal escapes, whatever --color says.`
