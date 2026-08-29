# Milestone 1 — Local skill management

Data mode: **local filesystem**. This milestone is the first implementation line and is integrated through `feature/milestone-1`.

[`PROCESS.md`](PROCESS.md) governs branch ancestry, sequential chunk PRs, review, verification, consolidation, and plan retirement. Root [`PLAN.md`](../PLAN.md) is the executable implementation plan; this file settles the outcome and acceptance boundary.

## Capability

`esheep` reads Agent Skills from human-maintained local source directories and renders declarative, non-command-enabling copies into enabled Claude Code, Pi, Codex, and optionally shared Agent Skills targets on macOS and Linux.

Users control how source directories are created and updated. Esheep treats them as read-only inputs and performs no source preparation or command execution.

## Acceptance boundary

- A built Go binary reports a meaningful version, has Cobra commands and completion, and is delivered from HEAD only.
- Defaults, the human-owned settings file, `ESHEEP_*` target variables, and target flags follow documented precedence. Settings and source directories are never written by the CLI.
- `[[sources]]` TOML tables define safe named local directories. HOME/XDG resolution is hermetic; source and enabled-target roots are absolute, disjoint, non-nested, and bounded.
- `sync` discovers immediate non-symlink child directories containing `SKILL.md`, while skipping dot-directories and `node_modules`; validates recognized skill contents and the declarative skill subset; rejects command-enabling metadata; detects case-insensitive collisions; renders stable per-target output; copies supporting files as non-executable data; installs atomically; writes source/skill/target ownership markers; and prunes only validly marked stale output on enabled targets.
- Every target block supports `disabled`. Claude and Pi additionally support `argument-hint`; Codex and agents have no additional metadata. Claude and Pi renders contain common fields plus `argument-hint`, while Codex and shared renders contain common fields only. Root `.esheep.toml` is the only reserved output path.
- `skills list` reports source and per-target `synced`, `drifted`, `missing`, or `disabled` state.
- `config` emits redirectable effective TOML, resolved paths, and optional provenance. `lint` reports all parser, declarative-subset, symlink, name, and collision violations with the documented exit behavior.
- Unmarked, mismatched, and symlinked destination directories remain untouched. Disabled targets remain untouched. Failed swaps roll back.
- README and CLI help document supported platforms, local-source ownership, target ownership, output streams, exit statuses, configuration, and HEAD installation.

## Required acceptance proof

From a clean temporary environment, an agent runs the exact milestone-wide workflow in root `PLAN.md` against two plain local fixture directories. The workflow uses the built binary and isolated `HOME` and `XDG_CONFIG_HOME`; proves version/configuration and no settings or source mutation; exercises collisions, all render variants, supporting files, markers, unmarked-directory preservation, drift repair, exact pruning, declarative-metadata rejection, lint failures and recovery, optional shared-target installation, and the representative `config → sync → skills list → lint` user flow.

`make check` must run equivalent durable subprocess coverage from `e2e/`.

Nothing beyond the checked root-plan chunks is complete.
