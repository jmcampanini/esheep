# Development process

This is the canonical contract for taking an esheep milestone from plan to
`main`. `plans/MILESTONES.md` is the roadmap, the active `plans/MILESTONE_N.md`
settles outcomes and acceptance, and root `PLAN.md` is the temporary executable
implementation and verification plan. The completed milestone file and root
plan are retired, not archived, in the post-review wrap-up before the milestone
merges to `main`.

## Branch roles and ancestry

- `main` contains reviewed, consolidated milestones.
- The milestone branch starts at the current `main` tip, integrates every chunk
  for one milestone, and eventually opens a PR to `main`. Branch names may use
  `feature/milestone-N`.
- Each chunk branch starts from the current milestone-branch tip and opens its
  sequential PR to that milestone branch, never `main`. Branch names may use
  `feature/milestone-N-chunk-K`. After a chunk is squash-merged, the next chunk
  starts from the resulting integration tip.
- The milestone branch is deleted after its reviewed PR is squash-merged into
  `main`; chunk branches are deleted after their PRs merge.

Agents must not commit, push, open/update PRs, or merge unless the user
explicitly requests that specific action.

## Planning and chunks

Plan the active milestone before implementation. Review its acceptance boundary,
then write root `PLAN.md` with dependency-aware vertical chunks. Every chunk
must add a human-exercisable capability unless a genuinely necessary
review-only slice is explained; two review-only chunks may not be consecutive.

Root `PLAN.md` begins with one unchecked progress item per chunk. Each chunk
records its human outcome, implementation boundary and dependencies, primary
test owner, cheapest faithful verification, exact human-proof commands, and
agent verification. Check a chunk only after those items, its proof, and local
`make check` pass. Review, CI, merge, and user acceptance are separate gates.

A chunk demo is `.sandbox/demos/<milestone>-chunk-<n>.html`: a self-contained
arrow-key HTML deck containing verbatim output from the real built binary and
fresh temporary state. It is disposable and never committed. Review-only
chunks produce no demo.

## Delivery workflow

1. Review `plans/MILESTONE_N.md` and approve root `PLAN.md`.
2. Create the milestone branch from `main`.
3. For each chunk in order: branch from the integration tip, implement the
   slice and its primary tests, build the real binary, run its exact proof and
   capture the demo, run `make check`, then stop for user review. The user
   commits, opens the chunk PR to the milestone branch, requires green CI,
   reviews the demo, and squash-merges it.
4. After all chunks merge, run the complete root-plan workflow from a clean
   environment with the real binary and retain its transcript. Equivalent
   durable subprocess coverage belongs in `e2e/` and runs in `make check`.
   The user performs the milestone user-story demo.
5. Open a consolidation PR to the milestone branch that reconciles the
   implementation, tests, specifications, temporary divergences, and roadmap;
   rerun the complete workflow, require green CI and review, and squash-merge it.
6. Run the foundation review against the milestone branch after consolidation
   merges, then classify every accepted finding as fix-now or scheduled.
7. Open a wrap-up PR to the milestone branch. Resolve fix-now findings, record
   scheduled work as the next milestone's chunk-0 manifest (or explicitly
   record that no chunk 0 exists), codify lasting guardrails, update links, and
   delete the completed milestone file and root `PLAN.md`; require green CI and
   review, then squash-merge it.
8. Open the milestone PR to `main`, require green CI, review, and squash-merge.
   Delete the milestone branch only after that merge.

## Divergence protocol

`plans/DIVERGENCES.md` is temporary intake only. Record an actual contradiction
with a canonical or settled specification immediately, including a monotonic
ID, affected section, temporary behavior, owner, and consolidation deadline.
Spec-silent extensions belong in the active plan instead. Before consolidation,
reconcile each entry into the active milestone and relevant durable docs, align
code/tests, verify it, and delete the resolved entry. Never use the intake as a
parallel specification or permanent decision archive.

## Consolidation and exit gates

The consolidation PR audits the milestone's affected product and documentation
contracts, reconciles implementation/tests/specification, resolves every due
divergence, and updates the roadmap. The foundation review is a fresh review of
the milestone branch; accepted findings are fixed before the milestone PR or
scheduled for the next milestone. Guardrails go into `AGENTS.md`, build/lint
configuration, or tests, not review memory.

A milestone exits only when all apply:

- every chunk had review, demo watch when applicable, local `make check`, green
  CI, and sequential squash-merges;
- durable `e2e/` coverage and the agent's clean real-binary transcript pass;
- the user demoed the milestone stories;
- canonical docs and temporary divergences are reconciled;
- foundation findings are fixed or carried as a complete next-milestone
  manifest;
- lasting review guardrails are codified;
- completed `plans/MILESTONE_N.md` and root `PLAN.md` are deleted, links pass;
- final `make check`, CI, review, and squash-merge into `main` pass.
