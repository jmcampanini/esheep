# Milestone 1 implementation plan

## Progress

- [x] Chunk 1 — Foundation and local source configuration
- [x] Chunk 2 — Discovery, validation, and rendering
- [x] Chunk 3 — Ownership-safe synchronization and inspection
- [x] Chunk 4 — Documentation and milestone verification

## Goal

`esheep` is a macOS- and Linux-only Go CLI that reads Agent Skills from human-maintained local source directories and renders them for Claude Code, Pi, Codex, and optionally the shared Agent Skills directory.

Users decide how source directories are created and updated. Esheep never creates, updates, deletes, or executes content from a source directory.

Representative workflow:

```text
esheep config
esheep skills list
esheep sync
esheep skills status
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

Each configured source root is a read-only collection of skill directories. Discovery inspects only immediate non-symlink child directories containing `SKILL.md`; the source root and grouping directories are not skills. Dot-directories and `node_modules` are skipped.

Once a skill is recognized, its contents are traversed to validate supporting files. Supporting paths must be unique under case-insensitive Unicode-normalized comparison. A supporting-file symlink is valid only when it is relative, acyclic, and resolves within that skill root. Absolute, escaping, cyclic, and directory symlinks are errors. Supporting files are installed as non-executable data.

Missing, unreadable, or non-directory source roots are command errors for commands that consume skills. Metadata commands and configuration reporting do not require source roots to exist.

### Declarative skill format

A source skill contains YAML frontmatter and a Markdown body. Supported common fields are:

- `name`: required, lowercase alphanumeric with single hyphens, 1–64 characters, matching the directory name;
- `description`: required and nonempty, at most 1024 characters;
- `license`: optional string;
- `compatibility`: optional string, at most 500 characters;
- `metadata`: optional string-to-string map.

Command hooks, `allowed-tools`, and policy fields that grant or broaden execution permissions are validation errors. Unknown fields fail loudly. Esheep never silently strips an unsupported field and never executes source content.

Optional per-target blocks are `claude`, `pi`, `codex`, and `agents`. Every block may contain `disabled: true`. Claude and Pi may additionally contain the inert `argument-hint` field. Codex and agents have no additional metadata fields in Milestone 1. Executable hooks and permission policy are invalid in every block.

The Markdown body is preserved byte-for-byte. Rendering is deterministic:

- Claude and Pi receive supported common fields plus `argument-hint` when present.
- Codex and the shared target receive only supported common fields.
- Supporting files are rendered as non-executable data.
- Root `.esheep.toml` is reserved for ownership metadata and invalid in a source skill.

### Collisions

Skill names compare case-insensitively across all sources. A collision installs nothing for that name, reports every source, continues processing unrelated skills, and causes a nonzero final status.

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
- `esheep skills list [--json]`;
- `esheep skills status [--json]`;
- `esheep completion <bash|zsh|fish|powershell>`;
- `esheep --version`.

`sync` discovers all sources, validates and renders skills, reports collisions while continuing unrelated work, installs enabled targets atomically, prunes stale owned output, summarizes results, and exits nonzero when any skill fails.

`skills list` inventories every discovered source skill with `ready`, `invalid`, or `collision` readiness. It is observational and exits nonzero only when configuration or filesystem failures prevent complete discovery.

`skills status` reports source readiness and each target's `synced`, `drifted`, `missing`, `disabled`, or `blocked` state. Invalid and colliding source skills have no target state. It exits nonzero unless every source skill is ready and every target is synced or disabled. An occupied unowned, mismatched, symlinked, or case-colliding destination, or a target that cannot be inspected safely, is blocked. `--json` emits one complete uncolored document with structured diagnostics for either inspection command.

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

- Discover immediate non-symlink skill directories in each configured source.
- Parse and validate the declarative skill subset and supporting-file symlinks.
- Reject command-enabling metadata and `.esheep.toml` output collisions.
- Detect case-insensitive cross-source collisions.
- Render deterministic Claude, Pi, Codex, and shared output with non-executable support files.

Primary tests:

- Parser/validator unit tests own frontmatter and declarative-subset rules.
- Filesystem integration tests own top-level discovery and symlink behavior.
- Renderer goldens own exact target output.

Human proof: create two plain local fixture sources containing top-level valid, invalid, disabled, and colliding skill directories; run focused discovery/render tests and inspect rendered trees under `.sandbox/`.

Agent verification: run parser, discovery, symlink, collision, and renderer tests; execute the fixture proof without reading or writing outside `.sandbox/`; then run `make check`.

## Chunk 3 — Ownership-safe synchronization and inspection

**Human outcome:** `skills list` inventories known source skills, `sync` installs and repairs only esheep-owned skills, `skills status` reports deployment health, and disabled targets remain untouched.

Implementation:

- Add strict marker parsing for source, skill, and target.
- Add same-filesystem staging, rollback-capable swaps, and cleanup.
- Add drift comparison, exact marked pruning, and unowned-directory refusal.
- Wire `sync`, source-only `skills list`, and deployment-focused `skills status` with deterministic human output, JSON inspection, and aggregate failures.

Primary tests:

- Installation integration tests own marker validation, atomic failure recovery, symlink refusal, drift repair, and exact pruning.
- Command tests own output streams and aggregate exit status.
- Built-binary e2e owns complete wiring across real local source and target directories.

Human proof: inventory and synchronize two local fixture sources into isolated Claude, Pi, and Codex targets; inspect healthy status; edit an installed file and inspect unhealthy status; add an unmarked directory, remove a source skill, disable a target, rerun sync, and verify repair, preservation, pruning, disabled-target immutability, and restored health.

Agent verification: run installation and command tests; execute the complete isolated real-binary lifecycle proof; inspect every target and marker; then run `make check`.

## Chunk 4 — Documentation and milestone verification

**Human outcome:** Documentation matches the complete local-source workflow, command surface, ownership boundaries, streams, exit statuses, and failure behavior.

This is a review-only slice because final documentation and milestone acceptance reconciliation depend on the completed behavior from Chunks 1–3. It produces no chunk demo.

Implementation:

- Complete README command, local-source workflow, configuration, ownership, stream, exit-status, and failure documentation.
- Reconcile the milestone acceptance boundary and roadmap with the delivered command surface.
- Confirm the existing focused and built-binary coverage forms the milestone verification workflow.

Primary verification:

- Documentation tests and review own command, link, path, ownership, stream, exit-status, and failure consistency with CLI help and observable behavior.
- Focused configuration, discovery, validation, rendering, installation, management, command, and UI tests own their settled contracts.
- Durable e2e owns the built-binary `config → skills list → sync → skills status` workflow, isolated HOME/XDG behavior, validation and collision failure and recovery, every target render variant, settings and source immutability, target ownership boundaries, drift repair, exact pruning, and disabled-target preservation.

Human proof:

```sh
set -eu
before=".sandbox/$(namo --prefix m1-c4-status-before).txt"
after=".sandbox/$(namo --prefix m1-c4-status-after).txt"
git status --short >"$before"
make check
./build/esheep --help
./build/esheep completion --help
./build/esheep config --help
./build/esheep skills --help
./build/esheep skills list --help
./build/esheep skills status --help
./build/esheep sync --help
git diff --check
git status --short >"$after"
diff -u "$before" "$after"
```

Agent verification: run the exact human proof, inspect the documentation-test and real-binary e2e results within `make check`, and confirm the help output, documented command surface, local-source workflow, and tracked working tree remain consistent.
