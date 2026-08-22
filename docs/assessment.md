# Command Centre — assessment

**Date:** 2026-08-20 · **Assesses:** `command-centre-v1.md` (revision 2)

## What's in this folder

| File | What it is |
|---|---|
| `command-centre-v1.md` | The design doc, revision 2. The thing being assessed. |
| `command-centre-v2-buildability-review.md` | Full staff-engineer review, 466 lines. The detail behind §1 below. |
| `assessment.md` | This file. Verdict summary, competitive comparison, adaptability analysis, user's-eye view. |

Source of truth for the design remains `treepad/docs/command-centre-v1.md`. These are copies taken 2026-08-20.

---

## 1 · Buildability verdict

**BUILD IT AFTER FIXING 6 THINGS.**

The primitives are sound: the level-triggered tick, the task table with an audit log beside it,
pid liveness with no timing rule, artifact-first classification, file-redirected output, never
merging, the `tp` subprocess boundary. Revision 2's corrections are real.

What's wrong sits *between* the primitives and the code — six mechanisms stated confidently that
don't work as written, **three of them introduced by revision 2's own fixes**. Each is cheap now
(mostly one or two lines of spec) and expensive in three weeks, because each decides a config
schema, a DB column, an ownership boundary, or an upstream contract — not a function body.

### The three structural findings

**1. The only remaining gate is wrong in three ways.** §8's `required_checks` as a flat `[]string`
"straight out of `.mergify.yml`" is invented — the real predicate is a boolean expression with `or`
branches, `check-skipped`, and a clause where an *absent* path-filtered check legitimately passes
(`services/.mergify.yml:51-57`), which contradicts invariant 11. Two of four `services` check names
are wrong. Worse, the verdict has **no expiry**: re-parenting a stacked descendant fires
`pull_request: edited`, which no test workflow listens for, so the head SHA is unchanged, old
check-runs stay green, and the tick re-derives the same green forever. `review me` on a descendant
can mean "passed CI against a base that no longer exists" — the sharpest cost of the settled
stacking decision, and §13.4 admits only half of it. Separately, `services` auto-reruns failed itest
shards three times (`itest-retry.yml:10-31`), so red there is often transient and §3.6 parks rows
that need no human.

**2. The Activity file has two owners with two meanings.** treepad reads it by *existence* for the
double-launch guard (`launcher/activity.go:26-30`, used at `batch.go:280`) and by *mtime* for run
state on a 90s window (`launcher/activity.go:32-46`). §8 redirects agent stdout elsewhere, so the
mtime freezes at spawn — on the mtime reading, §10's veto (the thing that makes the whole 15s tick
legal) expires 90 seconds into a 40-minute run. Invariant 6 also has the app *delete* the file,
re-arming `tp ui`'s launch keys over a worktree full of work — the exact double-launch that settled
decision 10 wanted prevented. **The only primitive here where being wrong is a rewrite**, because it
fails silently as two agents in one worktree.

**3. The intake contract reverses a shipped skill rule and fixes the rarer of two identical
defects.** §10 frames the fan-in change as "emit three fields"; it is actually a reversal of
`to-tickets/references/treepad-manifest.md:61-68`, which instructs the opposite. And **fan-out has
the same defect, unmentioned**: rule `:47` ("blocked by exactly one other joins that blocker's
chain, directly after it") is satisfied by *both* of two slices blocked by the same blocker, so both
land in one chain and the second is based on the first — silently serialising independent work.
Fan-out is the more common shape. One rule replaces three.

### The other three

- **The stale-root fix is a no-op.** `git fetch origin` then `tp new --base main` resolves the same
  commit, because `git worktree add` takes the local ref (`lifecycle.go:69`). This was the *sole*
  justification for relaxing invariant 4.
- **The `tp` calls are unscoped**, so `link` pushes branches with no task row and no denylist check,
  falsifying invariant 2; and teardown loops because `materialise` skips by branch existence.
- **`waiting on producer deploy`'s verb re-runs the agent** when a CI re-dispatch is what's needed.

### Three more false premises beyond §14's four — two used to *delete* something

| Claim | Reality |
|---|---|
| §13.3 "`[sync] include` … there is no denylist" | False — `!` exclusions exist (`internal/sync/sync.go:246-254`). An invariant was dropped on this. Also aimed at the wrong file: neither repo has a root `.env`; the secret-bearing file is `support-app/.env.development.local`, which is *not* synced. |
| `required_checks` "come straight out of `.mergify.yml`" | Invented. |
| §10 "Intake … both already exist" | The task table exists nowhere — treepad has no database at all. Only the manifest directory and `batch.Load` exist. |

Smaller: `pr_stale` is `omitempty`, making §3.3's guard unimplementable · `ReportEntry` lives in
`internal/treepad`, so the app's key contract is an unimportable unversioned struct's JSON · §2 omits
`/implement`'s `/code-review` step (`implement/SKILL.md:13`), which itself fans out parallel
sub-agents — real unbudgeted cost, though the no-push claim holds · §12 says no chain-depth threshold
is asserted when one ships at `treepad-manifest.md:56` · the "never imports `batch/`" rule is a
calcification stronger than what was settled: `batch/` is built as the reusable pure-predicate module
with `api_test.go` policing it, and the rule forbids the typed coupling while leaving every untyped
one in place.

**Next move:** settle these six, then the build spine. Not a PRD first.

---

## 2 · How this compares to the "command centre" genre

Sources reviewed: [MindStudio, *Manage Goals Not
Terminals*](https://www.mindstudio.ai/blog/ai-agent-command-center-manage-goals-not-terminals) ·
[MindStudio, *Managing Multiple Claude Code
Agents*](https://www.mindstudio.ai/blog/ai-command-center-managing-multiple-claude-code-agents) ·
[Medium, *The Age of Agent Command
Centers*](https://medium.com/@georgethomasm_89397/the-age-of-agent-command-centers-how-to-build-your-own-multi-agent-ai-system-7f1d63330776)
(**403 — bot-blocked, characterised from search results only, not read**).

**They solve a different problem with the same word.** Both MindStudio articles are about *managing
attention* — the problem is a human with twelve terminal tabs, and the answer is a board and a status
field. This design is about *managing correctness across unlanded work*. It sits roughly one layer
beneath both: everything they call "assign an agent, set status to `in_progress`" is where all 501
lines of the design live. Conversely, everything the design calls "the user reviews and merges" is
where their thinking lives.

### Convergent — mild evidence the shape is right

| Their concept | This design |
|---|---|
| Goals kanban: Queued → In Progress → Needs Review → Blocked → Done | 13-state machine, one page |
| Goal schema (`id, status, priority, dependencies, assigned_agent, artifacts`) | the task table |
| Dispatcher: poll → highest-priority goal with no unmet deps → assign | §3.10 top-up to `max_agents` |
| 3–5 concurrent agents; "rate limits matter" | `max_agents`; §13.11 rate limits are the wall |
| Execution / reasoning / blocked failure categories | `failed` / `needs you` / `waiting on producer deploy` |
| Non-overlapping edits parallelise, shared state serialises | chains vs separate chains |

Nobody copied anybody. That's the shape the problem has.

### Divergent — where this design is ahead

1. **Level-triggered vs edge-triggered.** Their dispatcher *writes* `in_progress` on assignment and
   trusts it thereafter. This tick re-derives everything from ground truth every 15s and explicitly
   never latches. Note the irony: the one place this design latches (the CI verdict with no expiry)
   is the bug the review found. Their design latches everywhere.
2. **Truth is artifacts, not self-report.** The deepest difference. Their agents emit "JSON envelopes
   with status, artifacts, confidence levels, blockers", or write their own progress into
   `/agent-context/agent-log/*.md`. This design refuses both: liveness is `kill(-pgid, 0)`, success
   is commits since *this run's* baseline SHA, and §3.4 says *alive → running, whatever the log says*.
   An LLM's "confidence: 0.8" is a made-up number, and an agent that dies mid-tool-call writes no
   envelope at all — the case their architecture cannot observe and this one handles as default.
3. **No LLM in the orchestrator.** The Medium piece's model is a lead orchestrator agent delegating
   to specialists — supervisor pattern, orchestrator itself a model. §3 is deterministic Go with a
   hard "not five actors — five sections of one loop". An LLM orchestrator cannot offer an invariant.
   This one has eleven.
4. **CI is the integration surface; they don't have one.** MindStudio: "test results surface in review
   cards." The Claude Code piece: no CI automation, daily human sync. This design does the actually
   hard part — rollup `head_oid` matching the pushed tip, named required checks, empty rollup never
   green — that both articles wave at.
5. **Git plumbing is a real layer.** Their entire git story is "each agent works on its own branch,
   review diffs before merge." No worktrees, no stacking, no rebase safety, no squash divergence.
   This design's hardest problems don't exist for them because they never let work stack on unlanded
   work — not cleverness, just accepting the review-latency idle §13.4 rejected.
6. **Cross-repo is designed, not absent.** Seams, drafts held until producer merge, `waiting on
   producer deploy`. Both articles are single-repo throughout. Most novel mechanism here — and per
   §13.1, the least verified.
7. **Their Claude Code piece launches agents by hand** — terminal tabs with a good init prompt, which
   is the exact thing the sibling article's title says to stop doing.

### Three things they have that this doesn't

- **Review ergonomics.** MindStudio has an actual policy — always review production / external-API /
  user-data changes, spot-check internal tooling and docs, auto-approve formatting and mechanical
  refactors with passing tests — plus a ~60-second decision budget and a spec for what a review card
  surfaces. This design is excellent on the machine's experience and thin on the human's: the page
  has states and verbs but doesn't say what a `review me` row *presents*. Cheap, purely additive.
- **Handoff notes.** `/agent-context/handoff-notes/for-[next-agent].md` — a parent's decisions
  reaching its descendant. Their version is naive (agent-written, unverified) but it's a concrete
  shape for the empty box in §12 ("parent decision-trace into descendant prompts", approach.md
  Tension 4).
- **Decomposition inside the loop**, so a goal that turns out too big can be re-decomposed. This
  design pushes decomposition entirely out to the planning skills — a cleaner boundary, but it has no
  answer when decomposition is *wrong*, and intake is independently the weakest seam. Their answer
  isn't better, but two independent critiques landing on the same spot is worth noting.

**Net: nothing in either article should change the design.** The convergence is reassurance; the
divergence is this design being ahead of the genre. The one thing worth stealing is the
review-policy / decision-budget framing — a page-design question, not an architecture one.

---

## 3 · Adaptability: an in-app review loop

**The idea:** review PRs from the Command Centre — fetch the diff, review there, post comments to
GitHub, then kick off a subagent that takes those comments and applies the fixes.

**Verdict: unusually well positioned.** Better positioned for this than for most of §12, because the
two primitives it needs already exist:

- **`re-run` is already "relaunch in the same worktree, incremental on the existing branch, handed a
  delta"** (§5). Today the delta is the composed-spec diff when a seam changed. Review comments are
  the same shape, same code path.
- **§3.9's seam-drift hash is already the attribution mechanism.** "Recompose, hash, compare against
  the hash stored with the last run, flag on mismatch." Swap the composed prompt for the set of open
  review threads and you get comment→fix attribution free — you know which feedback a run addressed
  and won't re-apply it.

The launch → liveness → classify → push spine needs **nothing new**. That's the expensive part and
it's done.

| Piece | Cost |
|---|---|
| Fetch diff + threads | One more `gh` read next to §3.3. Additive. |
| Post comments | New write, same shape as §3.5's `gh pr create`. Additive. |
| `changes requested` → `running` → `checking` → `review me` | ~2 states, 1 edge. §5 already has `checking → running` for `waiting on producer deploy`. Additive. |
| Comment/thread table | New table beside `tasks`. Schema addition, not a `tasks` change. |
| §1's division-of-labour table | **A real edit, not a stretch.** |

That last row matters. §1 currently assigns review to GitHub and says the app must never "decide a
ticket is done". A review surface makes the app a *review client*. Fine — but make it a written
decision, not an eroded boundary, because §1 is the spine every invariant leans on.

**The one real hazard, already solved once.** Auto-fix-from-comments has precisely the failure mode
CI-retry was rejected for: it pushes, cancelling in-flight checks, and can present a worse branch as
`review me`. The difference is legitimate — a human comment is a deliberate, terminal trigger,
whereas CI red is a machine event that may be transient (`services` reruns itest shards 3×). So
human-triggered auto-fix is defensible where auto-retry-on-red isn't. **Reuse §12's CI-retry guard
list verbatim:** no pending re-dispatch, capped context delivered as a file, refuse to auto-push a
denylisted diff.

The denylist also gets *more* load-bearing here, not less — an agent acting on human instructions has
licence to roam, so §12's deferred `quarantined` / `files` field becomes more attractive the moment
this is built.

Second-order benefit: comments arriving *while* a fix run is in flight is a nasty race in an
edge-triggered design and a non-event here — the tick re-derives the open-thread set every 15s.
Level-triggering paying rent.

---

## 4 · Adaptability: swapping the intake process (wayfinder)

Assessed against [Matt Pocock's `wayfinder`
skill](https://github.com/mattpocock/skills/blob/main/skills/engineering/wayfinder/SKILL.md) ([design
doc](https://github.com/mattpocock/skills/blob/main/docs/engineering/wayfinder.md)).

**Wayfinder is not an intake replacement — it sits upstream of intake.** The blog coverage buries the
load-bearing fact. From the skill itself: tickets are **decisions, not build slices** ("plan, don't
do"); when the frontier clears, wayfinder **explicitly stops** and hands off to `/to-spec` → then
`/to-tickets`.

So it terminates at **`to-tickets`** — the same producer intake already reads from. It's a drop-in
replacement for the `to-plan` / grilling stage, one layer *above* the boundary the app cares about.
**Swapping it in costs the Command Centre zero changes.** §1's layer split is doing its job.

Two caveats.

**(a) Intake isn't coupled to a contract yet — it's coupled to `to-tickets`' current output shape.**
Today the task table doesn't exist, the manifest schema has none of `repo` / `seams` / `blocked_by`,
and `Chain{Tickets []string}` (`batch/manifest.go:35-37`) has nowhere to hang them. So "intake is
swappable" is true *of wayfinder specifically* (because it routes through `to-tickets`) and not yet
true in general. Writing that contract down is already on the must-fix-before-code list; doing it
makes intake genuinely producer-agnostic rather than accidentally compatible.

**(b) There's a real insight in how wayfinder holds its DAG.** It uses the **tracker's native
blocking relationships** — a true DAG. This pipeline then *linearises* it into treepad chains,
lossily and knowingly (`treepad-manifest.md:61-68` instructs telling the user which dependency it
dropped) — and the task table's `blocked_by[]` reconstitutes a true DAG on the other side. **The
lossy step is in the middle of a DAG-to-DAG pipeline.** Specify the task table as reading the
dependency graph from the tracker/manifest directly rather than inferring it from chain order, and:
wayfinder-style tracker-native DAGs slot straight in, *and* the whole fan-in/fan-out bug class stops
existing. Chains become purely a *treepad* concern — how to lay branches out — not the carrier of
dependency truth. One contract decision that pays for itself twice.

**The one thing that would genuinely hurt.** If wayfinder's `research` tickets (AFK,
parallel-subagent) ever wanted to run as Command Centre rows, that pushes hard on the task table: a
research ticket has no repo, no worktree, no branch, no PR, no CI verdict — and the table and state
machine assume all five. Not additive. Not a reason to avoid it, but decide it before the schema
sets, not after.

---

## 5 · The user's-eye view

What using this actually looks like, as opposed to how it works.

**Your job is two things: write specs, review PRs.** Everything between is the app's problem. A third
thing appearing means the app has failed.

**Getting work in.** You don't add work in the app. Run spec → `to-plan` → (`to-seams` if it crosses
repos) → `to-tickets`. Tickets land; the app picks them up on its own. Nothing to click.

**Then you open one page.** `localhost`, one list, one row per ticket. Rows fill in as agents start.
You launch nothing.

**While it's running: do nothing.** `waiting`, `running`, `checking` need nothing from you. Don't
check on them.

**Two rows ever want you.** `review me` → PR is up, CI is green, go review it on GitHub and merge if
you like it. `needs you` → something is actually broken. Everything else is scrollable noise.

**Three buttons, total.**

| Button | When |
|---|---|
| `kill` | An agent's been running 40 minutes and you've decided that's too long. Your call — the app won't guess. |
| `re-run` | Row is stuck or failed, or you fixed the blocker. Same worktree, picks up where it was. |
| `remove` | Row is merged or dead and you want it gone. |

**Things to see and not panic about.** `waiting on producer deploy` — cross-repo, the other repo
merged but hasn't deployed; clears itself, ignore it. `seam changed` — shared contract moved, hit
`re-run`. `stack-stale` — a branch underneath moved, needs a human. `push failed` / `failed` —
`re-run`; twice means look properly.

**Every row hands you a terminal.** Worktree path, pgid, elapsed time, all copy-pasteable. `cd` into
the path and you're in the agent's actual working directory. The page never traps you — deliberate.

**Merging is always you, always GitHub.** The app never merges, never rebases, never decides
something's done. A ticket is done when you merge the PR.

**One knob:** `max_agents`. Down when you hit rate limits, up when you don't.

**End of session:** rows say `merged`, hit `remove`, worktrees go away.

**Later, not v1:** review inside the app (§3 above) — read the diff, leave comments, press a button
that sends an agent to fix them.
