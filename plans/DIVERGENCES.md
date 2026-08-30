# Temporary divergence intake

Durable documentation, current code, and tests settle completed behavior. When
a milestone is active, its milestone file and root `PLAN.md` settle that
milestone's acceptance and implementation behavior. This file is only an intake
queue for a temporary contradiction discovered during implementation; it is not
a parallel specification or decision archive.

## Protocol

1. Record an actual contradiction with a settled plan or durable specification
   as soon as it is discovered. Assign monotonically increasing IDs and include
   the affected section, temporary behavior or proposed decision, owner, and
   consolidation deadline.
2. Before that deadline, reconcile the decision into the active milestone file
   and relevant durable documentation, align code and tests, and verify the
   result.
3. Delete the resolved entry during consolidation. Git history preserves the
   temporary discussion; do not retain a closed section.

Spec-silent behavior that extends the target belongs in the active milestone
plan and does not need a divergence entry.

Entry format:

```text
## D-NNN: short title
- Recorded: date and planning/implementation context
- Diverges from: plan or durable document and section
- Temporary behavior or decision: what differs and why
- Owner: person responsible for reconciliation
- Consolidate by: milestone boundary
- Reconciliation: milestone and durable sections that must change
```

## Intake

_No divergences recorded. The next entry number is D-001._
