## Build and verification

- Run tasks with `mise run <task>`; do not call `go`, `golangci-lint`, or similar tools directly.
- Run `mise run check` before handing off changes; keep it read-only.
- Keep formatting, dependency updates, and generation in their separate write-mode tasks such as `mise run fmt` and `mise run tidy`.
- Bump a tool with `mise upgrade --bump <tool>`, run `mise lock` and `mise run check`, and commit `mise.toml`, `mise.lock`, and `.mise/locks/`.


## Repository design

- Support macOS and Linux only.
- Treat harness session transcripts as read-only inputs; write only under the configured memory root.
- Contact another machine only when `--remote` names it, and only to run esheep there over ssh; every other command stays offline.


## CLI behavior

- Use Cobra `RunE` handlers and explicit argument validators on every command.
- Keep payloads on stdout and diagnostics or progress on stderr.
- Avoid unexpected prompts and return actionable nonzero errors.
- Keep command help the canonical documentation of user-facing contracts, keep the README a landing page, and keep both consistent with observable behavior.


## Breaking changes

- Use clean-break mode for every breaking change until this instruction is removed from this file.
- Old forms may remain only in Git history and change records.
- Do not add compatibility guards, aliases, migration errors, tests, comments, or documentation that retain old forms.
