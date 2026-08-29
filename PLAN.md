# Milestone 1 implementation plan

## Progress

- [x] Chunk 1 — Foundation and local source configuration
- [ ] Chunk 2 — Discovery, validation, and rendering
- [ ] Chunk 3 — Ownership-safe synchronization and inspection
- [ ] Chunk 4 — Linting, documentation, and milestone verification

## Goal

`esheep` is a macOS- and Linux-only Go CLI that reads Agent Skills from human-maintained local source directories and renders them for Claude Code, Pi, Codex, and optionally the shared Agent Skills directory.

Users decide how source directories are created and updated. Esheep never creates, updates, deletes, or executes content from a source directory.

Representative workflow:

```text
esheep config
esheep sync
esheep skills list
esheep lint
```

## Settled behavior

### Configuration

The human-owned settings file is `$XDG_CONFIG_HOME/esheep/esheep.toml`, falling back to `$HOME/.config/esheep/esheep.toml`:

```toml
[[sources]]
name = "personal"
path = "~/Code/skills"

[[sources]]
name = "work"
path = "~/Code/work-skills"

[targets.claude]
enabled = true
path = "~/.claude/skills"

[targets.pi]
enabled = true
path = "~/.pi/agent/skills"

[targets.codex]
enabled = true
path = "~/.codex/skills"

[targets.agents]
enabled = false
path = "~/.agents/skills"
```

Sources are configured only through TOML. Target precedence is defaults, discovered or explicit TOML, `ESHEEP_*` environment variables, then flags. `--config PATH` replaces discovery and is required when supplied. The CLI never writes the settings file.

A supplied environment map is hermetic. `HOME` must be set and absolute. `XDG_CONFIG_HOME`, when set, must be absolute. Bare `~` and `~/` expand from `HOME`; other `~user` forms are invalid. Source and target paths must be absolute after expansion and never resolve from the process working directory.

Source names are case-insensitively unique safe slash-separated identifiers. Source roots are canonicalized through existing symlinks, case-insensitively unique, and non-nested. Enabled target roots are canonicalized, unique, non-nested, disjoint from every source root, and may not be the filesystem root or home directory. Disabled targets are never mutated.

`esheep config` emits redirectable effective TOML followed by resolved paths as comments. `--provenance` adds field-source comments. There are no sensitive configuration fields, but rendering retains a redaction boundary.

### Skill discovery

Each configured source root is a read-only discovery boundary. A directory containing exactly `SKILL.md` is a skill. Search at most six levels below each source root, skip dot-directories and `node_modules`, and never follow directory symlinks.

A supporting-file symlink is valid only when it is relative, acyclic, and resolves within that skill root. Absolute, escaping, cyclic, and directory symlinks are errors. Supporting files are installed as non-executable data.

Missing, unreadable, or non-directory source roots are command errors for commands that consume skills. Metadata commands and configuration reporting do not require source roots to exist.

### Declarative skill format

A source skill contains YAML frontmatter and a Markdown body. Supported common fields are:

- `name`: required, lowercase alphanumeric with single hyphens, 1–64 characters, matching the directory name;
- `description`: required and nonempty, at most 1024 characters;
- `license`: optional string;
- `compatibility`: optional string, at most 500 characters;
- `metadata`: optional string-to-string map.

Command hooks, `allowed-tools`, and policy fields that grant or broaden execution permissions are validation errors. Unknown fields fail loudly. Esheep never silently strips an unsupported field and never executes source content.

Optional per-target blocks are `claude`, `pi`, `codex`, and `agents`. Every block may contain `disabled: true`. Claude and Pi may contain inert presentation metadata such as `argument-hint`. Codex may contain inert interface display metadata. Executable hooks and permission policy are invalid in every block.

The Markdown body is preserved byte-for-byte. Rendering is deterministic:

- Claude and Pi receive supported common fields plus supported inert native fields.
- Codex receives the supported common fields and inert interface metadata in `agents/openai.yaml` when present.
- The shared target receives supported common fields plus the inert Codex interface file when present.
- `.esheep.toml` and generated target metadata paths are reserved output names and invalid in a source skill.

### Collisions

Skill names compare case-insensitively across all sources. A collision installs nothing for that name, reports every source, continues processing unrelated skills, and causes a nonzero final status. `lint` reports the same collision.

### Installation and ownership

Every managed skill directory contains `.esheep.toml` with exactly:

```toml
source = "personal"
skill = "review-pr"
target = "claude"
```

A directory is owned only when its marker is a regular file with valid fields matching the expected source, skill, and target. Symlinked destination directories or markers are unowned. Esheep never modifies or removes an unowned or mismatched directory.

Target roots may not be symlinks. Render each skill into a temporary directory under its target root, then perform a rollback-capable same-filesystem swap. Failed installation restores the prior directory and cleans temporary state. Fresh renders determine drift; no content hashes are stored.

Prune only validly marked installations whose source skill is missing, disabled for that target, or removed from configuration. Pruning applies only to enabled targets. Concurrent mutating commands are unsupported in Milestone 1.

### CLI contract

Use Cobra `RunE` handlers and explicit argument validators. Payloads go to stdout; diagnostics and progress go to stderr. Commands never prompt.

Exit statuses are:

- `0`: success;
- `1`: a valid command failed because of application state or input;
- `2`: invalid command usage or command wiring failure.

Commands are:

- `esheep config [--provenance]`;
- `esheep sync`;
- `esheep skills list`;
- `esheep lint`;
- `esheep completion <bash|zsh|fish|powershell>`;
- `esheep --version`.

`sync` discovers all sources, validates and renders skills, reports collisions while continuing unrelated work, installs enabled targets atomically, prunes stale owned output, summarizes results, and exits nonzero when any skill fails.

`skills list` reports source and each target's `synced`, `drifted`, `missing`, or `disabled` state. `lint` reports compiler-style diagnostics; errors exit nonzero and warnings exit zero.

### Build and distribution

The root `main.go` owns process exit, Cobra wiring stays in `cmd/`, and implementation stays under domain-focused `internal/` packages.

The Makefile default is `help`. `build` uses `-trimpath`, disables build VCS metadata, and injects `VERSION`, defaulting to `git describe --tags --dirty --always`. The remaining targets are `test`, `fmt`, `fmt-check`, `tidy`, `tidy-check`, `lint`, `version-check`, `vuln`, `check`, and `clean`. Repository-pinned tools run through `go tool`.

Distribution is HEAD-only in this milestone. `go install github.com/jmcampanini/esheep@main` uses Go build information when no linker version is injected. A Homebrew formula is later work.

## Chunk 1 — Foundation and local source configuration

**Human outcome:** A user can build the CLI, inspect effective local sources and target paths, generate completion, and receive deterministic exit behavior without any settings mutation.

Implementation:

- Scaffold the root command, version, completion, and configuration report.
- Load `[[sources]]` only from TOML and target settings through the approved precedence layers.
- Resolve hermetic HOME/XDG paths and enforce source/target boundaries.
- Support macOS and Linux only.
- Provide reproducible build and complete local verification targets.
- Document current behavior and later synchronization guarantees.

Primary tests:

- Command tests own argument validation, streams, exit categories, and metadata commands avoiding configuration loads.
- Configuration tests own precedence, provenance, hermetic environment behavior, source tables, path boundaries, and redirectable output.
- Built-binary e2e owns version injection, settings immutability, real configuration discovery, and process exit behavior.

Human proof:

```sh
mkdir -p .sandbox/m1-c1/home .sandbox/m1-c1/config/esheep .sandbox/m1-c1/source
cat > .sandbox/m1-c1/config/esheep/esheep.toml <<EOF
[[sources]]
name = "local"
path = "$PWD/.sandbox/m1-c1/source"
EOF
make build
HOME="$PWD/.sandbox/m1-c1/home" \
XDG_CONFIG_HOME="$PWD/.sandbox/m1-c1/config" \
./build/esheep --version
HOME="$PWD/.sandbox/m1-c1/home" \
XDG_CONFIG_HOME="$PWD/.sandbox/m1-c1/config" \
./build/esheep config --provenance
make check
```

Agent verification: run the command/config unit tests, the real built-binary e2e workflow, the exact human proof, and `make check`; inspect `git status --short` to confirm verification does not mutate tracked files.

## Chunk 2 — Discovery, validation, and rendering

**Human outcome:** Configured local sources are discovered and validated, and every supported target render can be inspected through deterministic tests.

Implementation:

- Add bounded source discovery with directory-symlink exclusion.
- Parse and validate the declarative skill subset and supporting-file symlinks.
- Reject command-enabling metadata and reserved output paths.
- Detect case-insensitive cross-source collisions.
- Render deterministic Claude, Pi, Codex, and shared output with non-executable support files.

Primary tests:

- Parser/validator unit tests own frontmatter and declarative-subset rules.
- Filesystem integration tests own discovery bounds and symlink behavior.
- Renderer goldens own exact target output.

Human proof: create two plain local fixture directories containing valid, invalid, disabled, and colliding skills; run focused discovery/render tests and inspect rendered trees under `.sandbox/`.

Agent verification: run parser, discovery, symlink, collision, and renderer tests; execute the fixture proof without reading or writing outside `.sandbox/`; then run `make check`.

## Chunk 3 — Ownership-safe synchronization and inspection

**Human outcome:** `sync` installs and repairs only esheep-owned skills, while `skills list` reports actual state and disabled targets remain untouched.

Implementation:

- Add strict marker parsing for source, skill, and target.
- Add same-filesystem staging, rollback-capable swaps, and cleanup.
- Add drift comparison, exact marked pruning, and unowned-directory refusal.
- Wire `sync` and `skills list` with deterministic output and aggregate failures.

Primary tests:

- Installation integration tests own marker validation, atomic failure recovery, symlink refusal, drift repair, and exact pruning.
- Command tests own output streams and aggregate exit status.
- Built-binary e2e owns complete wiring across real local source and target directories.

Human proof: synchronize two local fixture sources into isolated Claude, Pi, and Codex targets; edit an installed file, add an unmarked directory, remove a source skill, disable a target, rerun sync, and verify repair, preservation, pruning, and disabled-target immutability.

Agent verification: run installation and command tests; execute the complete isolated real-binary lifecycle proof; inspect every target and marker; then run `make check`.

## Chunk 4 — Linting, documentation, and milestone verification

**Human outcome:** Users can diagnose all configured sources without installation, and documentation matches the complete local-source workflow.

Implementation:

- Add `lint` with all parser, declarative-subset, symlink, name, and collision diagnostics.
- Complete README command, configuration, ownership, and failure documentation.
- Complete durable e2e coverage and reconcile milestone documents.

Primary tests:

- Linter tests own diagnostic semantics and exit behavior.
- Documentation/link checks own command and path consistency.
- Durable e2e owns the representative `config → sync → skills list → lint` workflow.

Human proof: run lint against fixtures containing each error category, repair them, rerun the complete lifecycle, and confirm all commands succeed.

Agent verification: run lint and documentation checks, execute the complete milestone workflow from a clean isolated HOME/XDG environment using the real built binary, verify source directories remain byte-for-byte unchanged and target ownership boundaries hold, run `make check`, and confirm the tracked working tree is unchanged by verification.
