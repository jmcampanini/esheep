# Milestone 1 — Skill management

Data mode: **local and cache-backed**. This milestone is the first
implementation line and is integrated through `feature/milestone-1`.

[`PROCESS.md`](PROCESS.md) governs branch ancestry, sequential chunk PRs,
review, verification, consolidation, and plan retirement. Root [`PLAN.md`](../PLAN.md)
is the executable implementation plan; this file settles the outcome and
acceptance boundary, not the internal package layout.

## Capability

`esheep` manages Agent Skills-spec repositories and syncs each discovered skill
as a rendered copy into enabled Claude Code, Pi, Codex, and optionally shared
`~/.agents/skills` targets. Users can register repositories without network
access, inspect effective configuration and synchronized status, render
harness-native metadata, preserve hand-managed files, prune only esheep-owned
installs, and lint invalid or cross-repository sources.

## Acceptance boundary

- A built Go binary reports `esheep version <v>`, has Cobra commands and
  completion, and is delivered from HEAD only.
- Defaults, the human-owned settings file, `ESHEEP_*` environment variables,
  and flags follow the documented precedence. Settings are never written by
  the CLI; the machine-owned registry is written only by repository mutation
  commands.
- `repo add`, `repo list`, and `repo remove` derive or honor repository names,
  reject duplicate identities, use XDG state/data paths, and do not fetch on
  registration. Removal deletes the clone and marked installs without touching
  unmarked directories.
- `sync` clones or fast-forwards repositories with system `git`, discovers
  bounded non-dot directories containing `SKILL.md`, validates source
  frontmatter, detects duplicate skill names, renders stable per-target output,
  copies supporting files, installs atomically, writes ownership markers, and
  prunes only marked stale/disabled/removed installs on enabled targets.
- Claude and Pi hoist their blocks; Codex emits spec-only `SKILL.md` plus the
  generated `agents/openai.yaml`; shared `agents` follows the specified Codex
  policy behavior; disabled targets are left untouched.
- `skills list` reports source and per-target `synced`, `drifted`, `missing`, or
  `disabled`; `config` emits redirectable effective TOML, derived paths, and
  optional provenance; `lint` reports all starter spec, frontmatter,
  harness-shape, and collision violations with the required exit behavior.
- README and repository/security/CI documentation explain the observable
  contracts, required `git`, filesystem ownership, output streams, exit
  statuses, configuration, and HEAD installation.

## Required acceptance proof

From a clean temporary environment, an agent runs the exact milestone-wide
workflow in root `PLAN.md` against two local fixture git repositories. The
workflow must use the built binary and isolated `HOME`, `XDG_CONFIG_HOME`,
`XDG_STATE_HOME`, and `XDG_DATA_HOME`; it proves version/config, no network on
registration, no settings-file write, repository add/list/remove, collision
reporting with continued installation, all render variants, markers and
supporting files, unmarked-directory safety, drift repair, exact pruning,
lint failures and recovery, optional shared-target installation, and the
representative `repo add → repo list → sync → skills list → lint` user flow.
`make check` must run equivalent durable subprocess coverage from `e2e/`.

Nothing in this acceptance boundary is complete at plan creation.
