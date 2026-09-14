# Plan: the peer conflict gate

> Source: `docs/adr/0010-one-conflicting-peer-at-a-time.md`, settled in a grilling session on
> 2026-09-13. No PRD; the ADR carries the decision and this plan carries the build.

## The starting point

Six tickets of the frontend-pass slice each had two blockers. Two or more blockers unlock only once
every blocker has merged, and the ticket is then cut from `main`, so all six were cut from the same
commit and ran beside each other. Every one of them edited `web/app.css`.

Merging any one re-conflicted the rest, and the same resolution got done again against a `main` that
had moved. On 2026-09-13 that cost three resolutions of the same conflict in one afternoon, plus one
near-miss: git silently merged `.phase-header` and `.line` into a single truncated rule, because the
two shared an opening `display: flex; align-items: baseline`, and left no conflict markers behind to
show it had.

ADR 6 already wrote the rule down and left it unenforced: "a wide rename runs alone". This plan
enforces it, for peers rather than for stacks.

Three things about the codebase make this smaller than it looks:

- **The comparison already exists.** `MergesCleanly` (`git.go:269`) runs
  `git merge-tree --write-tree --name-only`. Its one caller (`observe.go:106`) compares
  `origin/main`'s tip against one branch. Peer contention is the same function with different
  arguments.
- **The skill already exists.** `cc/skills/resolve-merge-conflict/SKILL.md` is 63 lines, agent
  runnable, sorts conflicted paths five ways, and ends `Push nothing`. No Go file references it.
  ADR 6 named wiring it up as the change that closes this loop.
- **The push path needs nothing.** `pushPushable` (`push.go:54`) pushes any run carrying a commit
  after its baseline. Whether a resolution reaches the remote is decided by whether the run commits
  it, so there is no new push code to write and none should be written.

---

## Technical design decisions

### A peer is not a sibling

A **peer** is any other ticket with an open branch based on `main` (`CONTEXT.md`). It implies no
shared blocker and no shared slice. Scoping to a slice would miss the case ADR 6 cared about, since
a wide rename conflicts with open work wherever it was authorised from. Stacked branches are
excluded: ADR 6's `conflictedBase` covers those already, and a stacked branch is not based on
`main`.

### The read is pairwise and cached on the tip pair

Observe runs `MergesCleanly` for each pair of open main-based branch tips. The result is keyed on
the ordered tip pair, so a tick where nobody pushed makes no git calls at all. Worst case is C(n,2)
calls on the tick after a push, against ADR 6's one per branch.

### The sweep runs in ref order, before the rows

Holding is decided by one pass over tickets sorted by ref, carrying the set of tickets already
ready. A ticket is held if it conflicts with a peer still in that set. Deriving everything first and
then applying holds gives the wrong answer: where A conflicts with B and B with C, but A and C do
not, that order holds C behind a B which is itself about to be held. The sweep runs before the row
loop (`server.go:476`), in the same position `prsByBranch` occupies at `:471`.

### The fact rides on `RunFact`, not `Facts`

`Facts.ConflictedBase` is the wrong model. `Status` calls `statusFromRun` (`plan.go:292`) before it
reaches the `ConflictedBase` switch, and a held ticket has always run and always has an open PR, so
a fact on `Facts` would never be read. `RunFact.ConflictingPeer` sits beside `ConflictsWithMain`
(`plan.go:254`), is populated in `runFactFor` (`server.go:662`) inside the `OutcomePush` block, and
is ranked in `statusFromPush` (`plan.go:342`) below `ConflictsWithMain` and above `VerdictReviewMe`,
which is the state it suppresses. `Preview` and `LaunchCandidate` receive `conflictedBase` as an
argument rather than through `Facts`, so neither is touched.

### `blocked` with a distinct reason, not a twentieth state

`conflictingPeerReason` mirrors `conflictedBaseReason` (`plan.go:381`) so the two refusals read
alike. A conflicted base already reuses `blocked` rather than earning a state, and this is the same
shape of fact.

### Go decides what may commit

Of the skill's five classes, one is decidable from the path alone: a file the build generates or a
golden fixture the tests regenerate. Only that class commits, and committing is what pushes.
Everything else parks with the worktree resolved and uncommitted, whatever the agent concluded.
Reading the classification out of the agent's own report would move the trust boundary inside the
run, which is where §7 spends two boundaries keeping it out of.

### Not in scope

- **Serialising at launch.** It would hold tickets that turn out not to conflict, which is most of
  them, and nothing can be compared before either ticket has a branch.
- **Retargeting a ticket onto its peer.** Considered and rejected: `retarget.go:19` skips
  main-based rows deliberately, and an implicit stack edge would make `Unlock` answer a question it
  does not ask.
- **Mergify.** Dropped entirely. cc forms no opinion about any repo's `.mergify.yml`.
- **Merging anything.** cc never merges. A held row is a sentence on a board, not a lock.
- **ADR 6's stacked-base gate.** Untouched. This sits beside it, not on top of it.

---

## Phase 1: the gate

**Covers**: the pairwise read, the ref-order sweep, the held state and its reason on the row.

### What to build

Add the pairwise peer read to observe. For every pair of open main-based branch tips, call
`MergesCleanly` and record whether they merge, keyed on the ordered tip pair so an unchanged pair
costs nothing on later ticks. The raw result belongs on `Observation` beside `ConflictsWithBase`
(`observe.go:42`); the ordering decision does not, because observe does not know ref order.

Add the sweep where ref order is known, before the row loop in `server.go`. Sort candidate tickets
by ref, carry the set already ready, and hold a ticket whose branch conflicts with a peer still in
that set. The output is one string per ticket, empty when clean, naming the peer it waits on.

Carry that string on `RunFact.ConflictingPeer`, populate it in `runFactFor`, and rank it in
`statusFromPush` below `ConflictsWithMain` and above `VerdictReviewMe`. Add `conflictingPeerReason`
next to `conflictedBaseReason` so the sentence names the peer.

This phase ships the whole of the fix the ADR is about. Phases 2 and 3 make the remaining
resolutions cheaper; this one stops them being repeated.

### Acceptance criteria

- [ ] Two open main-based branches that genuinely conflict leave the lower ref `review_me` and the
      higher ref `blocked`, with a reason naming the lower one
- [ ] Two open main-based branches that do not conflict are both unaffected
- [ ] Where A conflicts with B and B with C but A and C do not, A and C are both ready and only B
      is held
- [ ] A stacked branch is never held by this gate; `conflictedBase` still owns that case
- [ ] A tick in which no tip moved makes no `merge-tree` call for any pair
- [ ] Merging a held ticket out of order is not an error: the next tick re-derives, and the ticket
      that was ready lands in `conflicts_with_main`
- [ ] Table case in `internal/plan/status_test.go` covering the new rank order against
      `ConflictsWithMain` and `VerdictReviewMe`
- [ ] e2e txtar covering the hold and its release, modelled on
      `e2e/tests/pushed_branch_conflicts_with_main.txtar`
- [ ] `go test ./...` passes

---

## Phase 2: the verb parks

**Covers**: the `resolve` verb, the run against the skill, the resolved and uncommitted worktree.

### What to build

Add `VerbResolve` to the constants in `internal/plan/verbs.go` and offer it from
`ConflictsWithMain` in `plan.Verbs`, which today returns `{VerbRefresh, VerbClosePR}`. Add it to
`supportedVerbs` (`internal/cc/verbs.go:26`) so `handleVerb` accepts it.

Add `applyResolveIntents` to the ordered steps in `RunOnce`, modelled on `applyAbortIntents`
(`verbs.go:54`): take `PendingVerbIntents`, act per intent, then `ConsumeVerbIntent`. It belongs
beside `refresh`, which is what produces the conflicted state it acts on.

Spawn the run through `spawnRun` (`loop.go:452`), which already serves a non-launch caller in
re-run (`verbs.go:200`). The real work is `plan.Compose` (`compose.go:10`), which hardcodes
`"/implement " + t.URL` and is the only prompt shape there is. Parameterise it so a resolve run
composes against `cc/skills/resolve-merge-conflict/SKILL.md` instead. The deny settings
(`internal/cc/settings.go`) already block `Bash(git push:*)` and `Bash(gh:*)`, so nothing is added
there.

The run resolves and stops without committing. Because `Disposition` sees no commit after baseline,
`pushPushable` skips it and the row parks for a human, with the conflict already resolved in the
worktree. Pin down which state that is and make sure its reason says the resolution is waiting to be
read, not that the run failed.

### Acceptance criteria

- [ ] A row in `conflicts_with_main` offers `resolve`; no other state does
- [ ] Pressing it queues an intent that a later tick consumes exactly once
- [ ] The run is composed against the resolve skill, not `/implement`
- [ ] The agent cannot push: the spawn carries the existing deny settings unchanged
- [ ] After the run, the worktree carries the resolution and no new commit, and nothing is pushed
- [ ] The row parks at a state whose reason tells the operator the resolution is ready to read
- [ ] A resolve run that fails leaves the row readable and re-runnable, not stuck
- [ ] `go test ./...` passes

---

## Phase 3: generated files commit themselves

**Covers**: the one conflict class Go can prove is safe, and the push that follows from it.

### What to build

Add a path predicate naming what the build generates and what the tests regenerate:
`internal/cc/assets/dist/**` and `internal/cc/testdata/*.golden.html` today. Shape it like
`plan.Policy` in `plan/push.go:20`, per repo and configurable, because the generated paths are
repo-shaped and a hardcoded pair will not survive the second repo.

When every conflicted path in a resolve run satisfies that predicate, the run commits. `pushPushable`
then picks it up with no new code, the branch goes green, and the row returns to `review_me` without
the operator seeing it. When any conflicted path does not satisfy it, the run parks exactly as in
phase 2, whatever the agent thinks it resolved.

The generated-file case is the one that recurred all afternoon on 2026-09-13:
`internal/cc/assets/dist/app.css` is one minified line, so any two branches touching it conflict
unconditionally, and the resolution is always to regenerate rather than to merge.

### Acceptance criteria

- [ ] A conflict confined to generated and golden paths resolves, commits, pushes, and returns the
      row to `review_me` with no operator action
- [ ] A conflict touching one hand-written path parks, even when every other path is generated
- [ ] The predicate is per repo and configurable, not hardcoded to this repo's two paths
- [ ] The committed regeneration is byte-identical to running the build directly
- [ ] No new push code: `pushPushable` is unchanged
- [ ] e2e txtar covering the auto-committed case and the parked case
- [ ] `go test ./...` passes
