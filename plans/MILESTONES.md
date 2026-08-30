# Milestone roadmap

[`PROCESS.md`](PROCESS.md) is the workflow contract for active milestone planning, delivery, consolidation, plan retirement, and landing work in `main`.

## Completed

Completed milestones have permanently retired their temporary milestone files and root execution plans. Current code, tests, and durable documentation are authoritative for shipped behavior.

### Milestone 1 - Local skill management

Delivered the complete local skill-management loop for macOS and Linux: human-owned source and target configuration; declarative Agent Skills discovery and validation; deterministic Claude Code, Pi, Codex, and shared-target rendering; atomic installation; strict ownership markers; safe pruning; drift inspection; human and JSON reporting; completion; versioning; and a reproducible build and verification contract.

The foundation review centralized source-name and Unicode path-comparison rules, replaced platform-specific manifest syscalls with verified `os.Root` reads, documented the install transaction protocol, and added actionable usage guidance. Focused and real-binary tests preserve those guardrails.

## Deferred foundation-review triggers

No Milestone 2 or chunk 0 is planned. The Milestone 1 foundation review retained these triggers as unscheduled roadmap inputs rather than immediate work:

- Revisit target-set change amplification when a fifth harness target is planned. Four explicit targets do not yet justify a generalized target model.
- Revisit `skills status` per-target rendering when usage moves beyond interactive-scale inventories or measured latency makes the current deterministic rerendering costly.

## Later, unplanned

- **Running-session management:** quick-switch and overview of coding agents across tmux sessions; registration versus process and pane inspection remains open.
- **Session history:** search previous sessions across harnesses by parsing each harness's session format into a canonical session log.

The roadmap remains intentionally small until another milestone's outcome and acceptance boundary are planned.
