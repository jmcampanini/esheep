## Build and verification

- Run tasks with `mise run <task>`.
- Do not call pinned tools such as `go` or `golangci-lint` directly.
- Run `mise run check` before handing off changes.
- Keep formatting, dependency updates, and generation in their separate write-mode tasks such as `mise run fmt` and `mise run tidy`.
- Follow this sequence when updating a tool:
  1. Bump it with `mise upgrade --bump <tool>`.
  2. Run `mise lock` and `mise run check`.
  3. Commit `mise.toml`, `mise.lock`, and `.mise/locks/` together.


## Product contracts

- Follow the supported platforms and safety properties in [README.md](README.md).
- Treat command help as the canonical documentation of user-facing contracts.
- Keep the README a landing page.
- Keep command help and the README consistent with observable behavior.


## Breaking changes

- Use clean-break mode for every breaking change until this instruction is removed from this file.
- Old forms may remain only in Git history and change records.
- Do not add compatibility guards, aliases, migration errors, tests, comments, or documentation that retain old forms.
