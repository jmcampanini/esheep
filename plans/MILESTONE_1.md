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
- `sync` discovers immediate non-symlink child directories containing `SKILL.md`, while skipping dot-directories and `node_modules`; validates recognized skill contents and the declarative skill subset, including support-path uniqueness under case-insensitive Unicode-normalized comparison; rejects command-enabling metadata; detects case-insensitive collisions; renders stable per-target output; copies supporting files as non-executable data; installs atomically; writes source/skill/target ownership markers; and prunes only validly marked stale output on enabled targets.
- Every target block supports `disabled`. Claude and Pi additionally support `argument-hint`; Codex and agents have no additional metadata. Claude and Pi renders contain common fields plus `argument-hint`, while Codex and shared renders contain common fields only. Root `.esheep.toml` is the only reserved output path.
- `skills list` inventories every discovered source skill and reports source readiness. `skills status` reports per-target `synced`, `drifted`, `missing`, `disabled`, or `blocked` state and acts as a deployment health check. Both inspection commands support one-document JSON output.
- `config` emits redirectable effective TOML, resolved paths, and optional provenance.
- Unmarked, mismatched, and symlinked destination directories remain untouched. Disabled targets remain untouched. Failed swaps roll back.
- README and CLI help document supported platforms, local-source ownership, target ownership, output streams, exit statuses, configuration, and HEAD installation.

## Required acceptance proof

From a clean tracked worktree, an agent runs `make check`. Focused tests prove configuration boundaries; discovery, collision, declarative-metadata, supporting-file, and symlink validation; all target render variants; atomic installation, marker ownership, rollback, drift, and exact pruning; command streams and exit behavior; and deterministic human and JSON output.

The durable subprocess tests build the real binary and create clean environments under `.sandbox/`. The configuration workflow uses isolated `HOME` and `XDG_CONFIG_HOME` to prove version identity, effective configuration, precedence, settings immutability, and configuration and usage failures. The milestone workflow uses two plain local fixture directories to prove validation and collision failure and recovery, `config → skills list → sync → skills status`, all target render variants, supporting files, settings and source immutability, markers, disabled targets, unmarked-directory preservation, drift detection and repair, and exact pruning.

After `make check`, the agent inspects root and subcommand help, checks documentation links and command references, and confirms verification introduced no tracked changes.
