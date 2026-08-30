## Build and verification

- Use the Makefile targets for local development and verification.
- Run `make check` before handing off changes; keep it read-only.
- Keep formatting, dependency updates, and generation in their separate write-mode targets.
- Keep the production build reproducible with trimpath, disabled build VCS metadata, and commit-derived version identity.
- Use the repository-pinned Go tools through `go tool` rather than global installations.

## Repository design

- Keep the root executable entrypoint in `main.go`.
- Keep Cobra command wiring in `cmd/`, with one command per file where practical.
- Keep application-specific implementation under `internal/`.
- Support macOS and Linux only.
- Treat configured source directories as human-managed, read-only inputs; never access the network or create, update, or delete source directories.
- When root `PLAN.md` exists for an active milestone, treat it as the implementation authority and keep its settled behavior aligned with the CLI.

## CLI behavior

- Use Cobra `RunE` handlers and explicit argument validators on every command.
- Keep payloads on stdout and diagnostics or progress on stderr.
- Avoid unexpected prompts and return actionable nonzero errors.
- Keep `--help`, README documentation, and observable behavior consistent.

## Skill synchronization

- Never modify an unmarked skill directory during install or prune operations.
- Install managed skills atomically and record `source`, `skill`, and `target` ownership in `.esheep.toml` markers.
- Leave disabled targets untouched during synchronization and pruning.
- Preserve supporting files as non-executable data and reject escaping, absolute, cyclic, or directory symlinks.
- Reject command hooks, `allowed-tools`, and policy fields that grant or broaden execution permissions.

## Breaking changes

- Use clean-break mode for every breaking change until this instruction is removed from this file.
- Old forms may remain only in Git history and change records. Do not add compatibility guards, aliases, migration errors, tests, comments, or documentation that retain them.
