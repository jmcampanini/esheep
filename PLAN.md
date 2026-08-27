# PLAN — Milestone 1: Skill management

This temporary root plan is the executable implementation plan. The outcome
and acceptance boundary are authoritative in [`plans/MILESTONE_1.md`](plans/MILESTONE_1.md);
the roadmap is [`plans/MILESTONES.md`](plans/MILESTONES.md), and workflow is
[`plans/PROCESS.md`](plans/PROCESS.md). Preserve the settled decisions below;
do not re-litigate them. Progress is recorded below.

## Progress

- [x] Chunk 1 — Foundation and repository management: scaffold/build/CI/security, root/version/completion, config, repo add/list/remove
- [ ] Chunk 2 — Skill synchronization
- [ ] Chunk 3 — Ownership lifecycle
- [ ] Chunk 4 — Inspection and validation

## Branch flow

`feature/milestone-1` is the integration branch, based on `main`. In sequence,
`feature/milestone-1-chunk-1`, `feature/milestone-1-chunk-2`,
`feature/milestone-1-chunk-3`, and `feature/milestone-1-chunk-4` branch from the
latest integration tip. Each chunk PR targets `feature/milestone-1`, never
`main`; the next chunk starts only after the prior PR is squash-merged. The
consolidated milestone PR targets `main`. Agents do not commit, push, open or
update PRs, or merge unless the user explicitly requests that specific action.

## References

- Fleet standards: `~/Code/github.com/jmcampanini/fleet/main/wiki/`, especially
  `build-system.md`, `go/repository-layout.md`, `go/build-and-quality.md`,
  `go/release-and-distribution.md`, `go/github-security.md`,
  `go/github-pull-requests.md`, `cli/command-contracts.md`,
  `cli/configuration.md`, `cli/terminal-output.md`, `cli/documentation.md`,
  `cli/patterns.md`, and `agent-instructions.md`.
- Agent Skills specification: https://agentskills.io/specification and
  https://agentskills.io/client-implementation/adding-skills-support.md.
- Harness examples: `~/.claude/skills/codex-ask/SKILL.md`,
  `~/.pi/agent/skills/write-pr-body/SKILL.md`, and
  `~/.codex/skills/.system/review-agent/` (`SKILL.md` plus `agents/openai.yaml`).

## Goal

`esheep` is a Go CLI that manages agent skills from multiple git repositories
and syncs them, rendered per harness, into Claude Code, Pi, Codex, and
optionally the shared `~/.agents/skills` directory:

```text
esheep repo add git@github.com:jmcampanini/skills.git
esheep repo add git@github.work.biz:jcampanini/work-skills.git
esheep repo list
esheep sync
esheep skills list
esheep lint
```

## Settled design

### Canonical skill format

A skill repository contains Agent Skills specification-conformant skills: one
directory per skill containing `SKILL.md` (frontmatter and body) plus support
files. esheep extends the specification with optional top-level per-harness
frontmatter blocks: `claude`, `codex`, `pi`, and `agents`. Each block carries
native harness configuration and may contain `disabled: true`.

```yaml
---
name: codex-ask
# required; must match the directory name
description: Route a question…
# required; ≤1024 chars
license: MIT
compatibility: Requires codex
# optional; ≤500 chars
metadata:
  version: "1.1.0"
allowed-tools: Bash(git:*)
claude:
  argument-hint: "[low|medium|high|xhigh] <question>"
  hooks:
    PreToolUse:
      - matcher: "Bash|Write"
        hooks:
          - type: command
            command: "mkdir -p .codex-ask"
            once: true
codex:
  interface:
    display_name: "Codex Ask"
    short_description: "Second opinion from Codex CLI"
    default_prompt: "Use $codex-ask to answer the requested question."
  policy:
    allow_implicit_invocation: false
pi:
  disabled: true
---
```

### Rendering rules

Sync renders each skill once for each enabled target. Common output frontmatter
starts from only the present specification fields, in stable order:
`name`, `description`, `license`, `compatibility`, `metadata`, and
`allowed-tools`. The markdown body is preserved byte-for-byte. Native harness
keys follow source order after the common fields, while generated Codex YAML
uses schema order. Other skill files are copied as-is and harness blocks never
appear in output. `.esheep.toml` and `agents/openai.yaml` are esheep-owned output
paths; either path in a source skill is a validation error rather than being
silently replaced.

- `claude`: `SKILL.md` hoists `claude` keys except `disabled` to top-level
  frontmatter.
- `pi`: `SKILL.md` hoists `pi` keys except `disabled` to top-level frontmatter.
- `codex`: `SKILL.md` is spec-only. When `codex.interface` or `codex.policy`
  exists, generate `agents/openai.yaml` containing exactly those keys. This is
  the generated openai policy file users do not author.
- `agents`: emit the spec-only `SKILL.md`, plus `agents/openai.yaml` when a
  Codex block is present; it is harmless to other clients and serves Codex if
  it scans `~/.agents/skills`.

### Storage and configuration

| Kind | Default path | Ownership |
|---|---|---|
| Settings | `$XDG_CONFIG_HOME/esheep/esheep.toml` (`~/.config/...`) | Human-owned; CLI never writes |
| Repo registry | `$XDG_STATE_HOME/esheep/repos.toml` (`~/.local/state/...`) | Machine-owned; only repo add/remove edit it |
| Clones | `$XDG_DATA_HOME/esheep/repos/<slug>/` (`~/.local/share/...`) | Re-derivable cache |

Settings deliberately do not contain repositories:

```toml
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

The registry is:

```toml
[[repos]]
name = "github.com/jmcampanini/skills"
url = "git@github.com:jmcampanini/skills.git"
```

Use `github.com/jmcampanini/go-config-loader` per the fleet configuration
contract. Precedence is defaults < discovered `esheep.toml` < `ESHEEP_*`
environment variables < flags. Target overrides use `--claude-enabled`,
`--claude-path`, and equivalent target names, with corresponding
`ESHEEP_CLAUDE_ENABLED`, `ESHEEP_CLAUDE_PATH`, and equivalent variables. An
explicit `--config <path>` replaces automatic discovery, is required-if-given,
and fails clearly when unloadable; an absent discovered default is fine.
Document the repository's behavior for relative `XDG_CONFIG_HOME` as required
by go-config-loader#13.

Repository inputs include standard Git URLs, SCP-style sources, `file://`
URLs, and local paths. Identity derives `host/path` from a network URL,
stripping scheme, `user@`, and trailing `.git`; local paths derive their final
component. `git@github.com:jmcampanini/skills.git` becomes
`github.com/jmcampanini/skills`. `--name` overrides, including for local path
basenames that are not safe logical identifiers. Names are safe slash-separated
identifiers. Clone slugs flatten `/` to `-`, producing
`github.com-jmcampanini-skills`; distinct names producing the same slug are an
error.

### Ownership and pruning

Every installed skill directory gets only this ownership marker:

```toml
repo = "github.com/jmcampanini/skills"
skill = "codex-ask"
```

Prune only directories with a marker whose `(repo, skill)` is no longer present,
is disabled for that target, or whose repository was removed. Never touch an
unmarked directory. Drift compares an installed copy with a fresh render; no
hashes are stored. Registry writes are atomic; render to a temporary directory
and use a rollback-capable swap so failed sync cannot leave a partial skill.
Milestone 1 adds no cross-process lock, and concurrent mutating commands are
unsupported. Sync and prune affect enabled targets only; disabled target files
remain untouched and this behavior is documented.

### Collisions

The same skill name in two repositories is an error: install nothing for that
name, report both sources, continue other skills, and exit nonzero at the end.
`lint` flags the same collision.

### CLI contract

Use Cobra throughout, with `RunE`, explicit `Args` validators on every command
(`cobra.NoArgs` or `cobra.ExactArgs`), payloads on stdout, diagnostics/progress
on stderr, no unexpected prompts, actionable nonzero errors, shell completion,
and `--version` emitting `esheep version <v>`. List commands use deterministic
headered plain-text tables, lint uses compiler-style diagnostics, and Milestone
1 adds no separate JSON or structured-output mode.

- `esheep repo add <url> [--name <name>]`: validate/derive identity, reject
  duplicate name or URL, append registry, and never access the network.
- `esheep repo list`: list registered name and URL.
- `esheep repo remove <name>`: remove registry entry, delete clone, and prune
  that repo's marked installs from every enabled target.
- `esheep sync`: clone missing repos; otherwise fetch and fast-forward-pull the
  origin default branch without assuming `main`; discover, parse, validate,
  detect collisions, render/install per enabled target, prune, summarize, and
  exit nonzero for any skill error.
- `esheep skills list`: use cloned repos as source of truth and report skill,
  source repo, and each target's `synced`, `drifted`, `missing`, or `disabled`.
- `esheep lint`: errors exit nonzero and warnings exit zero. Rules include skill
  name grammar (lowercase alphanumeric plus single hyphens, 1–64 chars) and
  directory match; nonempty description ≤1024; compatibility ≤500; strictly
  string-to-string metadata; YAML frontmatter parsing; unknown top-level keys
  outside specification fields and harness blocks; unknown Codex keys, Codex
  interface/policy shapes, boolean disabled; warning for unknown Claude keys;
  and cross-repository collisions.
- `esheep config`: emit valid redirectable effective TOML, a concise derived
  values section with resolved targets/registry/clone paths, and field sources
  with `--provenance`. There are no sensitive fields in this milestone, but
  retain an honest redaction hook path.

### Git and discovery

Shell out to system `git`, respecting user SSH configuration including work
hosts; do not use a Go git library. Document `git` as a required external
program. In each clone, any directory containing exactly `SKILL.md` is a skill.
Search at most six levels below the repository root, skip every dot-directory
and `node_modules`, and never follow directory symlinks. Preserve a supporting-
file symlink only when it is relative, acyclic, and resolves within the skill
root; reject absolute, escaping, or cyclic links. No required repository layout
or per-repository manifest exists.

### Scaffolding and repository standards

Follow fleet standards: module `github.com/jmcampanini/esheep`, latest stable
Go (currently fleet baseline 1.26.5), root `main.go` owns process exit,
`cmd/<command>.go` has one command per file, and implementation is under
`internal/` in domain-focused packages (`config`, `registry`, `gitrepo`,
`skill`, `render`, `install`, `lint`), with Cobra limited to orchestration.
Parser/validator unit tests, renderer goldens, filesystem integration tests, and
built-binary e2e each own behavior at their closest faithful layer.

The Makefile has `help` as default, `build` with `-trimpath -buildvcs=false`
and version from `git describe --tags --dirty --always` injected into
`github.com/jmcampanini/esheep/cmd.Version` (`VERSION=x` override), plus
`install`, `test` (`-count=1 -race`), `fmt`, `fmt-check`, `tidy`, `tidy-check`,
`lint`, `version-check`, `vuln`, `check`, and `clean`. Pin golangci-lint and
govulncheck through the `tool` directive and use the wiki `.golangci.yml`
baseline.

CI has `.github/workflows/check.yml` exactly per the fleet wiki: a full-history
checkout, one required `check` job, and a porcelain read-only guard. It also
has a scheduled Friday 16:00 `America/New_York` vuln workflow plus manual
dispatch, and a PR Dependency Review workflow pinned to a full commit SHA,
read-only, context `dependency-review`, high/critical failure, all scopes, and
no license enforcement. Dependabot has weekly Friday 16:00
`America/New_York` Go-module and GitHub Actions updates, at most 10 open PRs,
required labels and commit prefixes (`deps(go)` and `deps(actions)`), with
fleet grouping; referenced labels are created.

`SECURITY.md` gives the private vulnerability-reporting route (GitHub private
vulnerability reporting is enabled) without a response-SLA promise. Root
`AGENTS.md` contains repository guidance bullets. `CLAUDE.md` first line is
exactly `@AGENTS.md`. Distribution is HEAD-only: no release machinery or
Homebrew formula in this milestone; README documents
`go install github.com/jmcampanini/esheep@main`.

GitHub settings already applied at bootstrap and must be verified, not redone:
public repository, default branch `main`, squash merge with `PR_TITLE`/`PR_BODY`,
`allow_update_branch`, read-only default Actions permissions, GitHub-owned
Actions restriction, secret scanning and push protection, private vulnerability
reporting, dependency graph/alerts, and Dependabot security updates. Remaining
ordered settings are: after the first green `check` on `main`, a `main` ruleset
blocking force push/deletion and requiring `check` (no reviewer requirement for
a sole maintainer); after representative runs, require `dependency-review`; and
once Go code exists, enable informational CodeQL default setup.

## Implementation and verification chunks

### Chunk 1 — Foundation and repository management

**Human outcome:** A built `esheep` binary can be safely introduced on a clean
machine: version and completion work, effective configuration is inspectable,
and local repository add/list/remove updates only the registry and clone state.
The settings file is never created or modified and repository registration does
not touch the network.

Implementation checklist:

- [x] Scaffold module, `main.go`, root Cobra command, version injection,
      completion, command error/stream conventions, and repository guidance.
- [x] Add Makefile, tools, lint baseline, build metadata, and initial `make
      check` target; add the check CI, scheduled vuln, dependency-review,
      Dependabot, security, and GitHub-settings verification artifacts.
- [x] Implement XDG path resolution and go-config-loader defaults/file/env/flag
      precedence, explicit-config errors, provenance/report rendering, and
      settings read-only behavior.
- [x] Implement registry model and atomic machine-owned registry writes;
      derive/override names, flatten clone slugs, reject duplicate names/URLs,
      and implement `repo add`, `repo list`, and `repo remove` without sync or
      prune behavior yet.
- [x] Establish the built-binary subprocess-test harness with isolated HOME
      and all XDG directories, local fixture repositories, no ambient network,
      and a durable `e2e/` entry invoked by `make check`.
- [x] Add initial README sections for install, version, config, repository
      commands, required `git`, output streams, and exit status.

Test ownership and cheapest faithful verification:

- [x] Root command, version, completion, argument validation, output streams,
      and config parsing are owned by fast command/config tests; config tests
      control environment and files explicitly.
- [x] Registry identity, duplicate rejection, serialization, and add/list/remove
      are owned by registry tests using temporary XDG paths; the subprocess e2e
      owns proof that the built binary and process boundaries are wired.
- [x] The first e2e fixture proves no settings-file creation/modification and
      no network by exercising local and remote-shaped repository registration
      under isolated environment variables.

Human proof (fresh shell; exact commands):

```sh
make build
REAL_PATH="$PATH"
mkdir -p .sandbox
T=$(mktemp -d "$PWD/.sandbox/chunk1-proof.XXXXXX")
export HOME="$T/home" XDG_CONFIG_HOME="$T/config" XDG_STATE_HOME="$T/state" XDG_DATA_HOME="$T/data"
mkdir -p "$HOME" "$XDG_CONFIG_HOME" "$XDG_STATE_HOME" "$XDG_DATA_HOME"
FIXTURE="$T/fixture"
git init -q "$FIXTURE"
git -C "$FIXTURE" config user.email test@example.invalid
git -C "$FIXTURE" config user.name test
mkdir -p "$FIXTURE/plain"
printf '%s\n' '---' 'name: plain' 'description: A local skill.' '---' 'Use it.' > "$FIXTURE/plain/SKILL.md"
git -C "$FIXTURE" add . && git -C "$FIXTURE" commit -qm initial
mkdir "$T/guard-bin"
printf '%s\n' '#!/bin/sh' "echo invoked >> \"$T/git-used\"" 'exit 99' > "$T/guard-bin/git"
chmod +x "$T/guard-bin/git"
export PATH="$T/guard-bin:$REAL_PATH"
before="$XDG_CONFIG_HOME/esheep/esheep.toml"
[ ! -e "$before" ]
./build/esheep --version
./build/esheep completion bash > "$T/esheep.bash"
head -n 3 "$T/esheep.bash"
./build/esheep config
./build/esheep repo add "$FIXTURE" --name local-fixture
./build/esheep repo list
./build/esheep repo remove local-fixture
[ ! -e "$before" ]
[ ! -e "$T/git-used" ]
```

The local URL, absence of `sync`, and registry-only commands demonstrate no
network access; the recorded demo captures the commands and output verbatim.

Agent verification:

- [x] Run formatter, tidy checks, lint, version check, vulnerability check, and
      `make check`.
- [x] Build the real binary and run the exact proof in a clean isolated HOME/XDG
      environment; inspect registry output and assert settings path does not
      exist or change.
- [x] Capture verbatim proof output in `.sandbox/demos/1-chunk-1.html` and
      verify only planning artifacts plus intended implementation files change.

### Chunk 2 — Skill synchronization

**Human outcome:** `sync` fetches local or remote repositories through system
git, discovers and validates skills, reports collisions while continuing, and
installs stable Claude, Pi, Codex, and optional shared-target renders with
supporting files.

Implementation checklist:

- [ ] Add git subprocess wrapper for clone, origin-default-branch discovery,
      fetch, and fast-forward update; preserve SSH configuration and avoid a Go
      git library.
- [ ] Add bounded dot-directory-skipping discovery and Agent Skills frontmatter
      parsing/validation, including all settled spec and harness fields.
- [ ] Add stable common frontmatter rendering, Claude/Pi hoisting, Codex
      `agents/openai.yaml` generation, shared `agents` output, disabled-target
      handling, supporting-file copying, and collision reporting.
- [ ] Extend the fixture and durable e2e to two local git repos containing plain,
      Claude/Codex, Pi-disabled, duplicate, and lint-violating skills; prove
      sync continues non-colliding installs after a collision.
- [ ] Extend README with sync, rendering, target, and git-fetch behavior;
      extend CI/check tooling only as needed while keeping `make check` green.

Test ownership and cheapest faithful verification:

- [ ] Git wrapper tests own command/result/error translation with local bare
      repositories; discovery, parser, validator, and renderer tests own their
      deterministic inputs and golden outputs.
- [ ] Collision and per-target output semantics are owned by renderer/sync
      integration tests; the built-binary e2e owns clone/update wiring, exit
      status, continued installation, and copied files.

Human proof (from a clean isolated environment, after creating two local git
fixtures with the skills listed above):

```sh
make build
export HOME="$T/home" XDG_CONFIG_HOME="$T/config" XDG_STATE_HOME="$T/state" XDG_DATA_HOME="$T/data"
./build/esheep repo add "$T/repo-a" --name repo-a
./build/esheep repo add "$T/repo-b"
./build/esheep repo list
./build/esheep sync; test $? -ne 0
find "$XDG_DATA_HOME" "$HOME" -name SKILL.md -print
cat "$HOME/.claude/skills/plain/SKILL.md"
cat "$HOME/.claude/skills/harness/SKILL.md"
cat "$HOME/.codex/skills/harness/agents/openai.yaml"
find "$HOME" -name .esheep.toml -exec cat {} \\;
```

- [ ] Agent verification: run parser/render golden tests, real local-git sync,
      collision assertions, all target assertions, and `make check`; capture
      `.sandbox/demos/1-chunk-2.html` with verbatim output.

### Chunk 3 — Ownership lifecycle

**Human outcome:** Installed state is safe to maintain: every esheep copy is
marked, marked drift is detected and repaired atomically, stale/disabled skills
are pruned exactly, unmarked skills survive every operation, and repository
removal cleans only that repository's owned state.

Implementation checklist:

- [ ] Implement marker read/write validation, temp-render-and-swap atomic
      installation, and failure cleanup.
- [ ] Implement enabled-target-only prune for missing, disabled, and removed
      `(repo, skill)` ownership; leave disabled-target files untouched.
- [ ] Implement `skills list` source-of-truth loading and synced/drifted/missing/
      disabled statuses.
- [ ] Complete `repo remove` clone deletion and marked-install pruning without
      touching unmarked directories.
- [ ] Extend README filesystem behavior with markers, drift, atomicity, prune
      safety, and disabled targets; extend durable e2e with edits, deletions,
      re-sync, remove, and unmarked-directory assertions.

Test ownership and cheapest faithful verification:

- [ ] Install/prune unit tests own marker parsing, ownership predicates,
      atomic-failure cleanup, and unmarked-directory safety using temp trees.
- [ ] Status tests own fresh-render drift semantics; subprocess e2e owns
      re-sync repair, exact pruning, clone removal, and lifecycle wiring.

Human proof:

```sh
./build/esheep sync; test $? -ne 0
mkdir -p "$HOME/.claude/skills/hand-managed"
printf 'name: hand-managed\\ndescription: keep\\n' > "$HOME/.claude/skills/hand-managed/SKILL.md"
./build/esheep skills list
printf '\nchanged\\n' >> "$HOME/.claude/skills/plain/SKILL.md"
./build/esheep skills list | grep drifted
./build/esheep sync
./build/esheep skills list | grep synced
# remove one source skill and commit it in the local fixture, then:
./build/esheep sync
# remove the registered repository:
./build/esheep repo remove repo-a
[ -f "$HOME/.claude/skills/hand-managed/SKILL.md" ]
```

- [ ] Agent verification: run lifecycle tests and built-binary e2e with a
      pre-existing unmarked directory, changed installed file, source deletion,
      disabled target, and repo removal; capture `.sandbox/demos/1-chunk-3.html`
      and pass `make check`.

### Chunk 4 — Inspection and validation

**Human outcome:** Users can inspect effective configuration and source/target
state, and `lint` explains every cross-harness or cross-repository problem with
reliable exit statuses. Documentation and a single durable end-to-end workflow
now describe and prove the complete milestone.

Implementation checklist:

- [ ] Implement all lint starter rules, warning/error classification, collision
      reporting, and actionable human diagnostics; verify unknown Claude keys
      warn while structural errors fail.
- [ ] Finish `config --provenance`, redaction hook path, target enablement, and
      derived-path reporting; verify relative XDG_CONFIG_HOME behavior is
      documented and tested.
- [ ] Add optional `agents` target e2e, fix the fixture then prove lint exits
      zero, and run the exact representative user sequence
      `repo add → repo list → sync → skills list → lint`.
- [ ] Complete README per fleet documentation requirements: purpose, HEAD
      install/upgrade, representative commands, required `git`, config
      discovery/precedence, filesystem ownership/pruning, stdout/stderr, and
      exit statuses.
- [ ] Perform final tooling sweep: format/tidy/lint/version/vulnerability/check,
      CI workflow validation, links and `rg` checks, and no settings writes.

Test ownership and cheapest faithful verification:

- [ ] Lint rule tests own individual stable violations and warning/exit
      contracts; config/report tests own redirectable TOML and provenance.
- [ ] The durable built-binary e2e owns complete wiring and the final user flow;
      it must run from `e2e/` inside `make check` with isolated state.

Human proof:

```sh
./build/esheep config --provenance
./build/esheep lint; test $? -ne 0
# fix the fixture's name, description, and unknown key, commit it:
./build/esheep lint; test $? -eq 0
cat > "$XDG_CONFIG_HOME/esheep/esheep.toml" <<'EOF'
[targets.agents]
enabled = true
path = "~/.agents/skills"
EOF
./build/esheep sync
./build/esheep skills list
./build/esheep lint
```

- [ ] Agent verification: run the complete isolated built-binary workflow,
      optional shared-target assertions, final `make check`, and exact-link and
      `rg` sweeps; capture `.sandbox/demos/1-chunk-4.html` and retain the clean
      transcript for milestone acceptance.

## Suggested implementation order

The four chunks are vertical but dependency ordered: Chunk 1 establishes the
binary, configuration, registry, and test harness; Chunk 2 makes a source skill
synchronize; Chunk 3 adds safe ownership and lifecycle behavior; Chunk 4 closes
inspection, lint, documentation, and complete acceptance. Within them, use the
fleet scaffolding, config/state, git/skill pipeline, install/prune, lint, and
E2E/documentation order without creating extra chunks.

## Milestone-wide agent-verified end-to-end workflow

This exact workflow is run after all four chunks against two local fixture git
repositories, using the built binary and a clean temporary environment. The
fixtures are created with `git init`, commits, and local paths; they contain a
plain skill, Claude hooks plus argument-hint, Codex interface plus policy, a
Pi-disabled skill, a duplicate skill name across repositories, and a lint
violation (bad name grammar, overlong description, unknown top-level key).

```sh
make build
T=$(mktemp -d)
export HOME="$T/home" XDG_CONFIG_HOME="$T/config" XDG_STATE_HOME="$T/state" XDG_DATA_HOME="$T/data"
mkdir -p "$HOME" "$XDG_CONFIG_HOME" "$XDG_STATE_HOME" "$XDG_DATA_HOME"
./build/esheep --version
./build/esheep config > "$T/config-report.toml"
./build/esheep repo add "$T/repo-a" --name repo-a
./build/esheep repo add "$T/repo-b"
./build/esheep repo list
./build/esheep sync; test $? -ne 0
./build/esheep skills list
./build/esheep lint; test $? -ne 0
```

The durable test then asserts all of the following:

1. The registry exists under the temporary state directory; the settings file
   was never created or modified. Registration of both local repos performed no
   network operation.
2. Sync reports both colliding sources, exits nonzero, and still installs every
   non-colliding skill into temporary Claude/Pi/Codex targets. Claude hoists
   `argument-hint` and `hooks`; Pi is spec-only and omits the Pi-disabled skill;
   Codex is spec-only and has the exact generated `agents/openai.yaml`;
   supporting files are copied and every installed directory has the correct
   marker.
3. A pre-existing unmarked target directory survives sync, prune, and
   `repo remove`.
4. `skills list` says `synced`; editing an installed file makes it `drifted`;
   sync restores it.
5. Removing a source skill and committing it causes sync to prune exactly its
   marked install. Removing a repository prunes that repository's remaining
   marked installs and clone.
6. Lint exits nonzero and names each fixture violation, then exits zero after
   the fixture is fixed.
7. Enabling `[targets.agents]` in the temporary settings file and syncing
   installs the spec-only shared copy and generated `openai.yaml` in temporary
   `~/.agents/skills`; the CLI still never writes the settings file.
8. From a fresh environment, the representative goal sequence
   `repo add → repo list → sync → skills list → lint` behaves as specified and
   its verbatim transcript is included in the completion report.

The agent also verifies `make check` is green, including durable subprocess
coverage from `e2e/`, and reports link/`rg` sweeps and the exact files changed.

## Consolidation checklist

- [ ] Reconcile README and all repository docs with shipped CLI, configuration,
      rendering, ownership, output, and exit-status behavior.
- [ ] Confirm no canonical product decision remains only in this temporary root
      plan; update durable docs and resolve every entry in
      [`plans/DIVERGENCES.md`](plans/DIVERGENCES.md).
- [ ] Reconcile tests with stable observable contracts and retain durable e2e in
      `e2e/` under `make check`.
- [ ] Verify GitHub settings in the settled order and codify review guardrails
      in repository instructions/configuration/tests.
- [ ] Run link and `rg` sweeps, final `make check`, the complete real-binary
      workflow, and the user milestone demo.
- [ ] After foundation review, fix findings or carry a complete next-milestone
      chunk-0 manifest with revisit triggers.
- [ ] Update `plans/MILESTONES.md`, retire this root `PLAN.md`,
      `plans/MILESTONE_1.md`, and `plans/DIVERGENCES.md`, and ensure no
      completed planning artifact remains as an alternative specification.
