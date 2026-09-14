# Plan: Command Centre — Phase 3 (cross-repo)

> Source: [`docs/prds/prd-command-centre.md`](../docs/prds/prd-command-centre.md) § Phase 3, § Concepts
> → A task (`seams[]`), § The states (`waiting on producer deploy`). Mechanism, state list and
> invariants are normative in [`docs/designs/command-centre-design.md`](../docs/designs/command-centre-design.md)
> (revision 4) — cited as `§n` and `inv. n` below. Where this plan and the design disagree, the
> design wins, except at the one place this plan says so explicitly and gives its reason.
>
> Predecessors: [`plans/command-centre-phase-1.md`](command-centre-phase-1.md),
> [`plans/command-centre-phase-2.md`](command-centre-phase-2.md). Their Go conventions, testing
> layers, on-disk layout and schema hold unchanged and are not restated.

Phase 3 goal: **a ticket in one repo can be built against an agreed-but-unlanded change in
another, and the app is honest about which of the two is holding it up.** Nothing here makes
cross-repo work correct — no check verifies a seam, and none is added. The whole phase is
coordination: put the agreement in the prompt, keep the consumer's PR unmergeable until its
producer lands, and give the one deterministically-red required check a name that says "not yet"
rather than "broken".

**Every criterion in this plan is a test, and two fixture repos in `e2e/` are what a gating edge
is expressed against.** Nothing here waits on the cutover, on either entry spike, or on a real
repo pair being configured. Running the finished thing against `support-app` × `services` is
easier once there is a finished thing to run, and it is not this plan's work.

## Technical design decisions

### A seam is a file. Nothing parses it, and `lands_at` lives in config

Seams live in `plain/.claude/seams/<name>` — workspace-level, uncommitted, user-edited (§6, §8).
The file is **pasted whole into the prompt**: no symbol addressing, no marker comments, no
parser, no schema. That is the design's rule and this plan keeps it.

**Deviation from design §6, stated deliberately: `lands_at` is declared in
`plain/.claude/command-centre.toml`, not inside the seam file.** §6 says "each seam declares
`lands_at`", which reads as a header in the file — but a header is a thing to parse, and the same
sentence three lines earlier forbids parsing. Config already carries every other declaration the
app reads (`[[task]]`, `[[repo]]`), `to-seams` can write a block there as easily as a header, and
keeping the file opaque means an amended seam can never break the app's reader:

```toml
[[seam]]
name          = "customer-tier-field"
producer_repo = "services"
lands_at      = ["packages/core-graphql/src/schema.ts"]
```

`producer_repo` and `lands_at` are read by exactly one thing, the retirement pointer (Phase 4).
Neither affects unlock, the draft gate, or the verdict.

### Compose stays pure; the shell reads the files

`plan.Compose(t Task, seams []string) string` already exists and is already called with `nil`.
Phase 3 changes no signature: `internal/cc` resolves `tasks.seams` → file contents and passes
them. `plan.Task` gains one field, `Seams []string`, so the pure package can render a preview row
and refuse a task naming an absent seam without touching the filesystem.

**An absent or unreadable seam file is a refusal, not an empty string.** Composing around a
missing file would bind consent to a prompt with a hole in it, and the hash would then match at
spawn time — the exact failure §4b exists to prevent. In the preview it is a `refused` row; at
spawn time it is a refused spawn with the reason on the row.

### `seam changed` is a flag, and its mechanism already ships

Consent is bound to content (§4b, inv. 8): `launch_members.prompt_hash` is written at
authorisation, the tick recomposes at spawn time and refuses on mismatch. That code is in
`internal/cc` today and is unreachable, because with `seams` nil the recomposition can never
differ. Phase 3 makes it reachable and gives the refusal a name on the page.

`seam changed` is **a flag on a row, not a state** (§5). It composes with `queued`, `review me`,
anything — so it is a separate column on the page and a separate field on the row model, never a
`plan.State` value. `plan.Status` does not learn about it.

### The composed prompt is stored as a file, not a column

`re-run` on a `seam changed` row is handed the before/after diff of the composed prompt (§5), so
the app has to keep the text, not only its hash. `schema.sql` is version 1 with no migration code
and `OpenStore` refuses a version mismatch — adding a column would brick every existing DB. The
text goes beside the run log the app already writes per run:

```
~/Library/Application Support/command-centre/plain/runs/<run-id>.jsonl     agent stdout (exists)
~/Library/Application Support/command-centre/plain/runs/<run-id>.prompt    the composed prompt
```

Path derived from `runs.log_path`, so no schema change and no new state-dir constant.

### Gating edges, and what the draft gate is made of

A **gating edge** is a `blocked_by` entry whose blocker lives in another repo. `plan.Unlocked`
already classes edges and already ignores gating ones (inv. 16) — this phase adds the second
consumer of that classification rather than a second classification.

```
draftGate(gating []Task, prs map[string]PRState, verdictGreen bool)
    → { draft bool, reason Reason }

  keep as draft while: any gating blocker's PR is not MERGED
                       or the verdict is not green
  ready when:          every gating blocker MERGED and verdict green      (inv. 13)
```

Pure, in `internal/plan`, table-tested. Two facts, both already observed each tick, and nothing
stored (inv. 14).

**Un-drafting is a reconciliation, not an event.** `PR.IsDraft` is already in the snapshot
(`bulkFields` selects `isDraft`), so the tick's job is "for every open PR whose draft state
disagrees with the gate, call `gh pr ready`" — idempotent, crash-safe, and the same shape as the
push plan (§3.8). No `undrafted_at` column, no latch.

**A draft PR still counts as OPEN for unlock** (§4). A consumer's own draft unlocks its same-repo
dependents exactly as a non-draft would; the draft gate constrains merging, not stacking. This is
the one place the two mechanisms could have been conflated and must not be.

**Which PRs open as drafts:** a task with at least one gating edge or at least one seam. A
single-repo task with neither is unaffected, which keeps Phase 1 and 2's behaviour byte-identical
on the golden page tests.

### `waiting on producer deploy` is a verdict value, and "sole red" is two evaluations

`compat_check` has been in `Repo` config since Phase 1 and is read by nothing. Inv. 12: the state
is entered **only** when the configured compat check is the sole red required check and every
other required check is green.

`internal/verdict.Evaluate` resolves a predicate tree; it does not enumerate reds, and giving it
a red-collecting traversal to answer one question is more machinery than the question is worth.
Instead, evaluate twice:

```
real    := Evaluate(p, in)
if real.Verdict == NeedsYou && cfg.CompatCheck != "" {
    forced := Evaluate(p, in.withForcedGreen(cfg.CompatCheck))
    if forced.Verdict == ReviewMe { → WaitingOnProducerDeploy }
}
```

Exact by construction — "green once the compat check is green, red otherwise" *is* the definition
of sole-red under a monotone predicate — and it reuses the resolver rather than growing one. Two
tree walks over a map of five entries is not a cost worth naming.

The new `verdict.Verdict` value maps into `plan` the way `BaseMoved` already does: a
`RunFact.VerdictWaitingOnProducer` bool, because `plan` cannot import `verdict`.

### `re-check` is `gh run rerun`, and the id comes from `detailsUrl`

No gating workflow in either repo declares `workflow_dispatch`, so there is nothing to
re-dispatch (§5, must-fix 6). The mechanism is `gh run rerun <run-id>`, and the id is already in
the snapshot: `CheckState.DetailsURL` is captured per check, and a check-run's details URL is
`https://github.com/<owner>/<repo>/actions/runs/<run-id>/job/<job-id>`. Parse the segment after
`/actions/runs/`; a URL that does not match that shape is a refusal naming what it got, never a
guess.

`re-check` is in the same class as `retry push`: no agent, no worktree write, one `gh` call, an
intent consumed by the loop.

**There is no attempts counter in the app**, so must-fix 6's "infrastructure-caused transitions
charge no attempt" is a no-op. Named here so nobody builds a counter to satisfy a clause that has
nothing to count.

### New states, verbs and flags

`exhaustive` in `.golangci.yml` turns each addition into a build failure at every switch that has
not handled it, which is how the call sites get found — the mechanism Phase 2 used for its three
states.

| State | Constant | Derived when | Verbs |
|---|---|---|---|
| `waiting on producer deploy` | `plan.WaitingOnProducerDeploy` | `RunFact.VerdictWaitingOnProducer` | `re-check`, `re-run` |

| Verb | Constant | Does |
|---|---|---|
| `re-check` | `plan.VerbRecheck` | `gh run rerun <id>` on the compat check's run |

| Flag | Where | Set when |
|---|---|---|
| `seam changed` | row model + page column, not `plan.State` | recomposed hash ≠ the authorised `prompt_hash` |
| draft | row model + page column | `PR.IsDraft` |

`Verbs(WaitingOnProducerDeploy)` is `[re-check, re-run]` — `re-run` stays because a consumer can
also be wrong about the seam, and the only way to find out is to rebuild against it.

### Module boundaries

| Package | Change |
|---|---|
| `internal/plan` | `Task.Seams`; `DraftGate`; `WaitingOnProducerDeploy` + `VerbRecheck`; `RunFact.VerdictWaitingOnProducer`; `Compose` unchanged. Stays stdlib-only (`api_test.go`). |
| `internal/verdict` | `WaitingOnProducerDeploy` value; `Input.CompatCheck`; the forced-green second evaluation. Stays pure. |
| `internal/gh` | `Ready(repoPath, branch)`; `RunRerun(repoPath, id)`; `Create` gains a draft argument; `ParseRunID(detailsURL)`. |
| `internal/cc` | `seams.go` (resolve `seams[]` → contents, and `lands_at` → producer `main`); `draft.go` (the reconciliation); config `[[seam]]`; the `re-check` intent and handler; the two new page columns; the composed-prompt file. |
| skills | `to-seams` (new); `to-tickets` emits `seams[]`. |

### Out of scope

Each of these is a §12 deferral with a re-entry trigger, not an omission:

- **Producer-side seam assertion** (grep the seam against `lands_at`). Re-enter when producer
  drift wastes consumer work more than once.
- **Detecting producer deploys automatically.** Re-enter when `waiting on producer deploy` rows
  sit unnoticed. Until then `re-check` is the knob.
- **A seam amendment loop.** Seam amendment is a user-invoked skill; the app's only job is to
  stop the hash matching, which it does for free.
- **Anything that verifies a seam.** §6: the seam mechanism's job is coordination, not
  enforcement.

---

## Phase 1: Seams into the prompt

**User stories**: PRD § Concepts → A task (`seams[]`); § Phase 3 → "seams as prompt context";
design §6 job 1.

### What to build

The whole path from a seam file to an agent's prompt, and nothing else.

`to-seams`, new: asks for the seam's name, producer repo, consumer repos and `lands_at`, writes
`plain/.claude/seams/<name>` with the agreed interface pasted in as the user wrote it, writes the
`[[seam]]` block into `plain/.claude/command-centre.toml`, and halts for approval before either
write (§10). `to-tickets` emits `seams = [...]` per ticket alongside `blocked_by`, still
upserting on `ticket_url`.

App side: `[[task]] seams` in config, persisted into the `tasks.seams` column that already
exists; `plan.Task.Seams`; `internal/cc/seams.go` resolving names → file contents against the
workspace seam directory; the resolved contents passed to `plan.Compose` at both call sites — the
preview and the spawn. The preview renders the composed prompt so the seam text is visible before
authorising, and a task naming an absent seam is a `refused` row saying which name failed.

### Acceptance criteria

- [ ] A task with two seams composes to `/implement <url>` followed by both files' contents, in
      config order, and `plan.Hash` of that is what `launch_members.prompt_hash` stores.
- [ ] A task naming a seam with no file is `refused` in the preview, naming the missing seam, and
      is never spawned.
- [ ] A seam file that is present but unreadable (mode 000) is the same refusal, not a panic and
      not an empty paste.
- [ ] `plan` still imports nothing outside the standard library (`api_test.go` unchanged).
- [ ] A task with no seams composes byte-identically to Phase 2 — the golden page and preview
      tests are untouched.
- [ ] `to-seams` writes both the seam file and the `[[seam]]` block, and halts for approval first.
- [ ] `to-tickets` emits `seams[]`, upserting on `ticket_url`, with a cross-repo example in its
      own fixtures.

---

## Phase 2: `seam changed`, and the diff a re-run is handed

**User stories**: PRD § The states → "`seam changed` is a flag on a row, not a state"; design §4b,
inv. 8.

### What to build

The refusal that already exists, made reachable and visible.

Write the composed prompt to `<run-id>.prompt` at spawn. Each tick, for every authorised member
with no run, recompose and compare against the stored hash; on mismatch the tick refuses to spawn
and the row carries the `seam changed` flag with the state it would otherwise have had. On a row
that has already run, the same comparison against the latest run's stored prompt sets the flag
too — a `review me` row whose seam moved under it is exactly the case the flag exists for.

`re-run` on a flagged row is handed a unified diff of the stored prompt against the recomposition,
written to a file and passed as the run's prompt preamble. Computing it is `os.ReadFile` plus a
diff over two strings; the flag is what makes it worth having.

### Acceptance criteria

- [ ] Editing a seam file while a member sits `queued` leaves the row `queued` with `seam changed`
      set, and the tick spawns nothing for it however many ticks pass.
- [ ] Re-authorising that task in a new launch clears the flag, because the new member's hash was
      taken from the current composition.
- [ ] A `review me` row whose seam is edited reads `review me` **plus** `seam changed` — the flag
      composes, and `plan.Status` gained no case for it.
- [ ] `re-run` on a flagged row spawns with a preamble containing the before/after diff, and the
      new run's `.prompt` file holds the new composition.
- [ ] Every run's `.prompt` file sits beside its `.jsonl` under the state dir, mode 0600, and no
      schema change was made (`meta.schema_version` still 1).
- [ ] A missing `.prompt` file (a run from before this phase) degrades to no diff and a logged
      event, never a failed `re-run`.

---

## Phase 3: Gating edges and the draft gate

**User stories**: PRD § Unlock → "a cross-repo edge feeds the Phase-3 draft gate, never unlock";
design §6 job 2, inv. 13, inv. 14.

### What to build

`plan.DraftGate` as specified above, and the reconciliation that acts on it: `gh pr create --draft`
for any task with a gating edge or a seam, and `gh pr ready` on any open PR whose draft state
disagrees with the gate. `internal/gh` gains `Ready`; `Create` gains the draft argument.

The page shows draft-ness as a column and, on a drafted row, the reason — "waiting on
`services#PLA-40`" or "waiting on its own checks" — because a draft that never lifts is otherwise
indistinguishable from one the app forgot about.

Two fixture repos in `e2e/`, since one repo cannot express a gating edge at all.

### Acceptance criteria

- [ ] A consumer task with a gating edge opens its PR as a draft; a single-repo task with no seams
      opens a non-draft PR exactly as in Phase 2.
- [ ] The consumer stays a draft while its gating blocker's PR is open, and while its own verdict
      is not green — both halves tested independently.
- [ ] The consumer is un-drafted on the tick after its gating blocker merges *and* its verdict is
      green, and `gh pr ready` is called exactly once across the following ten ticks.
- [ ] A gating blocker whose PR closes unmerged leaves the consumer drafted, with the reason
      naming the closure — never un-drafted, and never `base gone` (that state is for a *stacking*
      parent).
- [ ] A draft consumer's own same-repo dependents still unlock off its branch: draft counts as
      OPEN (§4), asserted directly.
- [ ] `gh pr ready` failing leaves the row untouched and the error on the page; the next tick
      retries, because nothing was latched.
- [ ] `internal/plan`'s `DraftGate` is table-tested over the four combinations of
      (blockers merged?) × (verdict green?).

---

## Phase 4: The retirement pointer

**User stories**: design §6 job 3 — "once the producer merges, later consumers are pasted the real
file from `main`, so the private seam file is never read again and cannot go stale".

### What to build

When every producer for a seam has merged, compose stops reading
`plain/.claude/seams/<name>` and instead reads each `lands_at` path from the producer repo's
`origin/main` (`git show origin/main:<path>` in the configured repo, from the snapshot the tick
already fetched). The seam file stays on disk untouched — it is the user's, and deleting it is not
the app's call.

This deliberately changes the composition, and therefore the hash. A launch authorised against
the seam file and not yet spawned reads `seam changed` and needs re-authorising — which is the
correct outcome, not a bug to work around: the text the agent would be handed genuinely changed,
from an agreement to the landed truth.

### Acceptance criteria

- [ ] Before the producer merges, compose pastes the seam file; after, it pastes `lands_at`'s
      content from `origin/main`, and the two compositions differ.
- [ ] A `queued` member authorised pre-merge reads `seam changed` post-merge and is not spawned.
- [ ] A `lands_at` path absent from the producer's `main` is a refusal naming the path — never a
      silent fallback to the seam file, which would paste stale text under a fresh hash.
- [ ] A seam with no `[[seam]]` block, or one with an empty `lands_at`, keeps pasting the file
      forever with no error: `lands_at` is optional and its absence means "no retirement".
- [ ] Multiple `lands_at` paths paste in declared order, and the ordering is asserted.
- [ ] The seam file is never written to or deleted by the app.

---

## Phase 5: `waiting on producer deploy`, and `re-check`

**User stories**: PRD § The states → `waiting on producer deploy`; § The six must-fixes row 6;
design §6 "what a cross-repo consumer actually experiences", inv. 12.

### What to build

`compat_check`, consumed at last. `verdict.Input` gains `CompatCheck`; `Evaluate` gains the
forced-green second evaluation above and the new `WaitingOnProducerDeploy` value. `plan` gains the
state and the `re-check` verb; `internal/cc` maps one to the other via
`RunFact.VerdictWaitingOnProducer`, parses the run id out of the compat check's `DetailsURL`, and
runs `gh run rerun <id>` when the intent is consumed.

The row shows which check is holding it and its details URL as plain text, because the human's
next question is "has the producer actually deployed", and the answer is not on this page.

### Acceptance criteria

- [ ] Compat check red, every other required check green → `waiting on producer deploy`.
- [ ] Compat check red **and** one other required check red → `needs you`, not the new state
      (inv. 12's "sole red", asserted as the negative case).
- [ ] Compat check red while another required check is still pending → `checking`, not the new
      state: a pending check is not a green one.
- [ ] A repo with `compat_check` unset never derives the state, whatever goes red.
- [ ] `re-check` calls `gh run rerun` with the id parsed from the compat check's `detailsUrl`, and
      the row returns to `checking` on the tick after.
- [ ] A `detailsUrl` that does not contain `/actions/runs/<id>` refuses the verb with the URL in
      the reason, and re-runs nothing.
- [ ] The bounded wait still applies: a row that sits in the new state past the wait does not
      silently become `needs you` — the state is entered from a *resolved* red, so the wait has
      already elapsed by construction, and that is asserted rather than assumed.

---

## Phase 6: The cross-repo slice, end to end

**User stories**: PRD § Phase 3 in full; the scenario the whole phase exists for.

### What to build

No new mechanism. Two fixture repos in `e2e/` — a producer and a consumer — one seam, one producer
ticket, one consumer ticket with a gating edge, driven through the full sequence in one testscript:
authorise both, producer runs and opens its PR, consumer runs and opens a **draft** PR, consumer's
compat check goes red → `waiting on producer deploy`, producer merges, `re-check` flips the compat
check green, consumer un-drafts and reads `review me`.

### Acceptance criteria

- [ ] The seven-step sequence above runs as one testscript against two fixture repos, asserting
      the state at each step rather than only the end.
- [ ] The consumer is never un-drafted before the producer's PR merges, at any point in that
      script.
- [ ] Killing the app mid-slice and restarting resumes it: nothing relaunches, no PR is
      duplicated, and no draft is lifted twice.
- [ ] The producer's own row is untouched by every draft-gate and compat-check step: a producer
      has no gating edges, so nothing in this phase may change how it behaves.
