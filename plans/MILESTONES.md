# Milestone roadmap

`plans/PROCESS.md` is the workflow contract. The active milestone file is
authoritative for product outcomes and acceptance; root `PLAN.md` is the
temporary executable implementation plan. Both are retired at consolidation.

## Active

| # | Milestone | Capability | Status |
|---|---|---|---|
| 1 | [Skill management](MILESTONE_1.md) | Synchronize declarative skills from local source directories across configured harnesses | Active |

## Milestone 1

Milestone 1 establishes the complete local skill-management loop: a Go CLI
scaffold and build/release baseline; human-owned local source and target
configuration; skill discovery, declarative Agent Skills validation, per-harness
rendering, atomic installation, ownership-safe pruning, and drift inspection.
Its acceptance boundary is defined in [`MILESTONE_1.md`](MILESTONE_1.md), with
implementation and verification in the temporary root [`PLAN.md`](../PLAN.md).

## Later, unplanned

- **Running-session management:** quick-switch and overview of coding agents
  across tmux sessions; registration versus process/pane inspection remains
  open.
- **Session history:** search previous sessions across harnesses by parsing
  each harness's session format into a canonical session log.

The roadmap is intentionally small until Milestone 1 is accepted. New
milestones enter here only after their outcomes and acceptance boundary are
planned.
