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

## CLI behavior

- Use Cobra `RunE` handlers and explicit argument validators on every command.
- Keep payloads on stdout and diagnostics or progress on stderr.
- Avoid unexpected prompts and return actionable nonzero errors.
- Keep command help the canonical documentation of user-facing contracts, keep the README a landing page, and keep both consistent with observable behavior.

## Skill synchronization

- Never modify an unmarked skill directory during install or prune operations.
- Install managed skills atomically and record `source`, `skill`, and `target` ownership in `.esheep.toml` markers.
- Leave disabled targets untouched during synchronization and pruning.
- Skip dot-prefixed supporting entries before validation at every depth, except the reserved skill-root `.esheep.toml` name.
- Validate `esheep-` prefixed source entries but never install them or their descendants. Expand harness includes from the owning root: the skill directory for skills or the source's `agents-md/` directory for agents files. Keep that root fixed through nested includes and symlinks, reject cycles, and preserve the affected destination when expansion fails.
- Apply the same include language to skill bodies and entire managed agents files. Optional includes insert zero bytes only for genuinely absent files; broken symlinks and other read or validation failures remain errors. Leave surrounding line endings unchanged and install empty rendered agents files.
- Treat sources as trusted: follow included symlinks wherever they resolve, and treat an included link that does not resolve or produces a directory cycle as an error.
- Discover managed global instruction files only under each source's `agents-md/` directory; container-root `AGENTS*.md` files are repository content esheep never reads.
- Preserve supporting files as non-executable data.
- Validate the frontmatter fields esheep interprets; pass every other top-level field through verbatim without granting it any meaning of esheep's own.

## Breaking changes

- Use clean-break mode for every breaking change until this instruction is removed from this file.
- Old forms may remain only in Git history and change records. Do not add compatibility guards, aliases, migration errors, tests, comments, or documentation that retain them.
