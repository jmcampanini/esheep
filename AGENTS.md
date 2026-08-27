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
- Shell out to the system `git` binary for repository operations.
- Treat `PLAN.md` as the milestone implementation authority and keep its settled behavior aligned with the CLI.

## CLI behavior

- Use Cobra `RunE` handlers and explicit argument validators on every command.
- Keep payloads on stdout and diagnostics or progress on stderr.
- Avoid unexpected prompts and return actionable nonzero errors.
- Keep `--help`, README documentation, and observable behavior consistent.

## Skill synchronization

- Never modify an unmarked skill directory during install or prune operations.
- Install managed skills atomically and record ownership in `.esheep.toml` markers.
- Leave disabled targets untouched during synchronization and pruning.
- Preserve supporting files when rendering skills for each enabled target.
