# Command Centre revision 3 — architecture review (Phase 1 foundations)

_Date: 2026-08-22. Read-only; no source file was modified. Target: `docs/command-centre-design.md`
revision 3 and `docs/prd-command-centre.md`, reviewed against treepad source, both plain repos
(`services`, `support-app`), the installed skills trees, live GitHub data (`gh pr list`/`gh api`
against real PRs), and `gh` 2.97.0's actual flag semantics. Seven parallel dimension reviews plus
one dedicated adversarial citation-verification pass. Headline question: **is Phase 1 safe to
build, or does it lock in foundations — schema, boundaries, the pure/impure split — that must be
reshaped at Phase 2/3?**_

---

## Verdict — BUILD IT AFTER FIXING 9 THINGS

**The primitives survive a third review.** The level-triggered tick, pid liveness with no timing
rule, artifact-first classification, the launch as the unit of consent, unlock and the verdict as
pure functions, never merging, stacking on open PRs — none of these needs rethinking, and several
of revision 3's deletions were verified *stronger* than claimed (squash is server-enforced in both
repos, so §4a's "history is worthless" premise is airtight; `refresh`'s merge commits do **not**
pollute a descendant PR's diff, which the doc never even claimed credit for).

**But the same failure mode that sank revisions 1 and 2 is present in revision 3.** The dedicated
verification pass checked 24 load-bearing claims: **5 FALSE, 5 stale/imprecise — and three of the
FALSE ones sit directly under a revision-3 deletion or a dissolved must-fix.** The single most
load-bearing call in the design cannot return the data half the invariants read. The design's
merge-time integrator is configured, in files already on disk, to exclude stacked PRs entirely.
And the trust boundary the push denylist defends is not the real one, because the agents arrive
with their own pre-approved `git push`.

None of the nine requires rewriting the tick, the task table, or the state machine — that is why
the verdict is not DO NOT BUILD YET. But six of the nine are schema, boundary, or contract
decisions that Phase 1 will encode wrongly and Phase 2 will have to invert. Fix them in the spec
first; almost all are paragraph-sized.

**Direct answer to the headline question:** Phase 1 as currently specced is *not* safe to build —
it would lock in (a) a PR-list contract that cannot see merged PRs, (b) an unlock function that
contradicts the phase plan it ships under, (c) a verdict expiry that either floods Phase 1 or
never runs in it, (d) a push trigger Phase 2's `refresh` must invert, and (e) run/push facts
hung off a schema Phase 2's re-runs break. After the nine fixes, Phase 2 and 3 are genuinely
additive over Phase 1's foundations.

---

## The recurring failure pattern, named loudly

Revisions 1 and 2 failed via line-accurate citations resting on false premises, used to delete
safeguards. Revision 3 repeats it. The three worst, each propping up a deletion or a dissolution:

1. **`gh pr list` defaults to `--state open --limit 30`** (verified, `gh` 2.97.0). The doc's
   literal command in §2/§3.2 — called "the single most load-bearing call in the design" —
   therefore cannot see a MERGED or CLOSED PR, and truncates below `support-app`'s **48 open PRs
   today**. This silently kills the OPEN-**or**-MERGED unlock gate (§4, inv. 16) — the very
   correction revision 3 made to revision 1's stranding bug — plus the `merged` state, `pr closed
   unmerged`, invariant 13's merged-producer test, and teardown's MERGED-PR evidence. treepad's own
   `PRList`, cited one line away at `internal/gh/gh.go:51`, passes `--state all --limit 200` with a
   comment explaining why.
2. **"Mergify's merge queue rebases onto current `main`"** (§1, §4a, §13.9, PRD) is FALSE — both
   repos set `update_method: merge` (`support-app/.mergify.yml:84`, `services/.mergify.yml:123`).
   This sentence is the stated justification for deleting all of revision 2's repair machinery.
   The *conclusion* survives (squash is server-enforced: `gh api` shows
   `allow_merge_commit: false, allow_rebase_merge: false` on both repos), but the premise printed
   in four places is wrong — and the mis-model hid the two facts that actually matter (§ below:
   `base=main`, branch deletion).
3. **"A stuck producer leaves its consumers parked indefinitely, by design"** (§6) is FALSE — both
   repos auto-close PRs at `updated-at<14 days ago` unless labelled `keep-open`
   (`services/.mergify.yml:184-197`, `support-app:137-150`). A parked row is closed by a robot,
   and §3.8 then cascades its dependents into a transition the state machine does not define.

Plus two carried over from the v2 review, still uncorrected in revision 3: §13.3's "no denylist
for the app to apply" (treepad `[sync]` supports `!` exclusions — `internal/sync/sync.go:246-256`)
and §12's "no threshold is asserted" for chain depth (one ships at `treepad-manifest.md:56-57`).
Two falsified claims surviving a review that named them is a process failure, not just content.

**What did *not* recur:** neither of the two user-litigated product decisions (stacking stays;
control plane, not scheduler) rests on any false premise found here. The product survives; the
mechanisms need revision 4.

---

## The nine must-fixes, in the order they must be settled

Each labelled by what kind of decision it is — **SCHEMA** (a column/config shape), **BOUNDARY**
(who owns what), **CONTRACT** (an interface's meaning), or **FUNCTION-BODY** (fix inside one
function).

### 1 · The PR-list contract — CONTRACT · Phase-1-blocking

The query as written (§3.2) fails four ways, all verified live:

- **Defaults**: `--state open --limit 30` — MERGED unreachable; 18 of `support-app`'s 48 open PRs
  invisible, and ordering is newest-first so the ones that fall off are the *oldest* — exactly the
  deep stack members. An invisible blocker reads as "no PR" → dependents to `blocked`; an
  invisible `checking` row has no rollup → bounded wait → `needs you`.
- **`--state all` doesn't fix it**: with thousands of merged PRs, `--state all --limit 30` on
  `support-app` returned zero merged rows (all 48 open PRs sort first), and `services` 504'd at
  `--limit 60` in live testing.
- **Missing fields**: `headRefOid` (invariant 11's `head_oid` binding has *no input* without it —
  rollup entries carry no per-entry oid), `baseRefOid` (the source for "the branch's current
  base"), `isDraft` (step 7 / invariant 13).
- **The rollup is a multiset of a union type**: live data shows `verify / Linear issue is linked`
  appearing **five times at one head SHA — two CANCELLED, three SUCCESS** (`support-app` #5750),
  and `StatusContext` entries with `name: null`. `{ success = "X" }` has no defined answer over
  this input, and §3.6's "anything else red" is *permanently true* on any PR that ever had a
  superseded run.

**Direction.** Two calls, stated explicitly: one bulk `--state open --limit 100` per repo for
rollups, plus one `gh pr list --state all --head <branch>` per *tracked* branch for unlock —
fan-out bounded by the app's own working set (~10), not the team's PR volume (verified working).
Write the full query and its Go struct into §3 as the design's central type, and state which
field each invariant reads.

### 2 · The verdict's input model and grammar — BOUNDARY + CONTRACT · Phase-1-blocking

- **Normalisation is a missing module.** The pure package's honest input is a *reduced* snapshot:
  latest-completed-per-name (by `startedAt`), union collapsed to one `CheckState`, plus
  `pushedTip`, `rollupHead`, `recordedBaseSHA`, `currentBaseSHA`, `pushedAt`. "Pure over
  `(predicate, rollup)`" as written threads GitHub's GraphQL wire shape into the core of the only
  gate. treepad models the fix one directory over (`internal/gh` mapping a private entry to a
  public type). → `internal/gh` as an anti-corruption layer; `internal/verdict` pure over the
  normalised snapshot; the reduction is the first table test.
- **The grammar cannot express its own motivating cases.** The real Mergify clause for the
  path-filtered check is a **negation** — `-check-neutral AND -check-pending AND -check-failure`
  (`services/.mergify.yml:54-56`) — which `absent_ok` only approximates, and unsoundly: from one
  snapshot the app cannot distinguish "path-filtered, will never appear" from "queued, hasn't
  appeared yet", so `absent_ok` reads green during the first seconds of every run — the exact hole
  invariant 11 closes at the rollup level, reopened per-check. And the `services` Linear clause is
  **four**-way, not three: `author = dependabot[bot]` (`:77`) is a PR attribute inside the "check
  subtree". → add `not`/`none_of` (or `pass_states`), make an absent-and-`absent_ok` check yield
  `pending` until a bounded wait elapses, and write `services`' full predicate out in the grammar
  in §8 — if it can't be written, that is the cheapest possible discovery.
- **"Red with a retry pending is `checking`" has no data source** — the revision-1/2 pattern in
  miniature. The rollup exposes no `run_attempt`; the retry workflow attaches to a *main* commit
  (verified: `headBranch: main` on every run; zero appearances in 40 sampled PR rollups). Eight of
  the last 30 `services` itest runs reached attempt 4; inter-attempt windows ran to 2h51m — so the
  row would *flap* between `checking` and `needs you` for hours. → delete the sentence (red is
  red; `re-run` is the verb; §12 already holds the deferral) or price the second call
  (`gh run rerun`-id parsed from `detailsUrl`). Do not ship the sentence with neither.
- **`re-check` names a mechanism that does not exist.** No gating workflow in either repo declares
  `workflow_dispatch` (verified — all bare `on: pull_request`). Must-fix 6's "Fixed" is fixed in
  name only. The working verb is `gh run rerun <id>` on the compat check's run — which happens to
  be exactly right, since it regenerates against production at run time. One flag's difference;
  say the real command.

### 3 · Unlock's rule — CONTRACT · Phase-1-blocking

- **§4 contradicts the PRD's Phase 1.** One blocker → "base = the blocker's branch" (stated twice,
  inv. 16), yet Phase 1 ships exactly that shape — two tickets, one blocking the other — with the
  second cut off `origin/main`. Same input, two different bases: Phase 1 either implements a
  different unlock than §4 (and Phase 2 replaces the function body plus every fixture — the
  opposite of additive) or it isn't the Phase 1 the PRD describes.
- **The MERGED arm produces an unlaunchable task.** `gh repo view --json deleteBranchOnMerge` →
  **true on both repos**. A merged blocker's branch does not exist; `tp new --base <it>` works
  only off a stale remote-tracking ref (the tick's fetch has no `--prune`), and
  `gh pr create --base <deleted branch>` fails outright. Revision 3's own "Merged counts too"
  correction creates this. → a merged blocker always yields `origin/main`.
- **Cross-repo edges break the blocker-count rule.** A task with one same-repo and one cross-repo
  blocker has two blockers, so §4 makes it wait for the producer to *merge* — contradicting §6's
  "producer and consumer run in parallel". And with exactly one cross-repo blocker, §4 hands it a
  base ref in another repository. → partition edges once, in Phase 1: **stacking** edges (same
  repo, feed the base) vs **gating** edges (everything else, feed the draft rule). Phase 1 = all
  gating; Phase 2 = stacking on; Phase 3 = classify by repo. Key the PR map by `(repo, branch)`.
- **Drafts count as OPEN.** Three of `support-app`'s eight newest PRs are drafts; unlock-on-OPEN
  treats a colleague's WIP — and, in Phase 3, the app's *own* draft consumer PRs — as a satisfied
  blocker. Unlock and the draft gate do not compose as written. Select `isDraft` and decide.
- **Unit mismatch:** one `base_ref` cannot serve both `tp new --base origin/main` (a ref) and
  `gh pr create --base main` (a branch). Carry a branch name; derive the `origin/` form for `tp`.

### 4 · The base lifecycle — SCHEMA + CONTRACT · Phase-1-blocking

- **The expiry is unscoped and floods Phase 1.** §4a scopes the base-SHA comparison to
  *descendants*; invariant 11 and §3.6 — the versions that become code — are unconditional.
  `origin/main` moved **130× in 14 days** in `support-app`, **187×** in `services`. So either
  every root task flips to `base moved` within about an hour of going green (making it the steady
  state of Phase 1, whose tickets are both roots), or — on the PRD's reading, "`base moved` and
  `refresh` are not reachable in Phase 1" — the expiry credited as the Phase 1 fix for must-fix 1
  never executes in the phase it is assigned to. The two documents contradict each other and both
  read as settled. → expire only when `base_ref != <repo default branch>`; being behind `main` is
  Mergify's job (`update_method: merge`, `Queue when ready` on `#commits-behind>=1` — the repos
  already answer this).
- **The base-gone case is the *normal terminal case*, and it is unhandled.** Parent merges →
  branch deleted (server setting) → `origin/<parent>` — the only argument `refresh` and the
  comparison ever take — ceases to exist. GitHub then auto-retargets child PRs, a behaviour the
  design never mentions (`grep -niE retarget` across both docs: one incidental hit). → give
  `base moved` two causes (parent advanced / parent merged); the second's verb is
  `gh pr edit --base main` + refresh-against-`origin/main`. Record base *kind* + ref + SHA, not a
  SHA alone.
- **`refresh` has no push path.** Invariant 1 pushes only a *dead run with commits since its
  baseline*; a `base moved` row has no running run, so the merge commit `refresh` creates lands
  locally and nothing ever pushes it — the verb that replaced all repair machinery is inert as
  specified. → restate the push step as reconciliation of `(local tip, pushed tip)` under the
  denylist, and invariant 1 as *what* may be pushed rather than *why*. Then initial push, re-run
  and refresh are one code path with three triggers.
- **`refresh` inherits a deleted precondition.** Mergify's `update_method: merge`, the
  `keep-up-to-date` label, and reviewers' "commit suggestion" clicks all push to the app's head
  branches — and `restack`, which reconciled a local branch with its own upstream
  (`git merge --ff-only origin/<branch>`), was deleted with nothing replacing it. `re-run` on a
  behind branch → non-fast-forward push → `push failed` → `re-run` loops. → both verbs begin with
  `git fetch origin <branch> && git merge --ff-only origin/<branch>`, refusing to `needs you` if
  not a fast-forward.
- **Conflicts and live agents.** A conflicted merge leaves `MERGE_HEAD` in a worktree no state
  covers; `re-run` would then hand the resolution to an agent — through the app's own "never
  merges" non-goal. And `refresh` is the only verb that writes to a worktree, with no invariant
  forbidding a git write under a live run. → one state (`refresh conflicted`, verbs: abort / open
  a shell) and one invariant (no git write into a worktree with a live run or unresolved merge).
- Small but real: invariant 4's "never deletes a worktree that **has commits**" forbids the
  `merged → remove worktree` teardown it coexists with — a merged worktree always has commits.
  The word wanted is *unpushed*.

### 5 · Mergify × stacks is answered, adversely, by config on disk — BOUNDARY · decide before code

§13.9 calls the queue interaction "the design's largest untested assumption" and defers testing to
Phase 2. Half of it is already answered, in the negative, by files in the repos:

- **`base=main` is the first condition of `common_checks` in both repos** (`:12` in each), and
  `queue_conditions`/`merge_conditions`/`merge_protections.if` all fold it in. **A stacked PR can
  never enter the merge queue and gets no merge-protection check-run at all.** The mechanism §4a
  delegates all integration to is structurally unreachable for exactly the PRs that need it.
- **The "Merge when ready" rule has no base condition** (`services:131-137`,
  `support-app:122-128`) — labelling a stacked child `ready-to-merge` squash-merges it **into its
  parent branch**, with no required checks, silently collapsing two review units on a shared team
  repo. The page's "merge bottom-up" display invites exactly that click.
- Verified live: `Mergify Merge Protections` = FAILURE on three PRs whose every gating check was
  SUCCESS — §8's refusal to read that check as the verdict is *confirmed with real data*, and it
  holds even harder for stacked children.

**Direction.** Own the retarget: when the app observes a parent MERGED, `gh pr edit --base main`
each child (the flag exists, verified). Add an invariant: the app never labels, and the page warns
against labelling, a non-`main`-based PR `ready-to-merge`. Add `--label keep-open` to
`gh pr create` (one flag standing between the design and the 14-day auto-close). Then run the
cheap spike (below) for the remaining genuinely-unknown half: queue behaviour on a *retargeted*
child.

### 6 · A tick that can be partially applied — BOUNDARY · Phase-1-blocking, not retrofittable

The v2 review's item ("`gh` exited non-zero ⇒ skip GitHub-derived transitions; promote to an
invariant; do it with the loop") was not adopted — revision 3 contains zero occurrences of
non-zero/exit/outage/timeout/backoff/idempotent. Verified blast radius of one transient 504
(reproduced live against `services` within three attempts): every `checking` row's bounded wait
fires → `needs you` en masse; every open-blocker dependent reads "no PR" → `blocked` — a state
whose definition ("unauthorised") contradicts their situation and drops the launch consent, with
no diagram edge covering the transition and only per-row `re-run` as recovery. This is a
loop-shape property — read-all → decide → act, one abort point after the read — and retrofitting
it later is a rewrite of `loop.go`, not error handling.

In the same shape decision:

- **Measured tick cost at Phase 1 scale is already ~95% of the period**: `gh pr list` with rollup
  5.8–6.0s on `services`, 2.9s on `support-app`; fetches 2.4–3.0s → **~14.3s of 15s** for two
  repos, before any pushes, PR creates, or `tp new` (whose `[sync]` clone is clonefile-fast on
  APFS but falls back to a byte-copy — `support-app` syncs `node_modules/` at 2.0GB/155k files,
  `services` syncs `.sst/` at 28GB). State sleep-after-work, split poll (per-repo cadence into an
  aged snapshot) from act, and move `tp new`/push/PR-create off the loop goroutine.
- **SQLite has no concurrency story** (WAL/busy_timeout/single-writer: zero mentions), and HTTP
  verbs race the tick by construction — a `cancel` landing between unlock (step 3) and launch
  (step 10) means the tick spawns from a launch the user just cancelled; the flock is per-process
  and all writers share the process. → WAL + busy_timeout + *only the loop writes*; verbs insert
  intent rows the next tick applies. That closes the cancel race, makes every verb idempotent,
  and deletes the redundant per-agent exit goroutine the v2 review already flagged.
- **Crash-mid-tick**: crash between `gh pr create` and the DB write → next tick re-creates →
  errors → `push failed` on a branch that is pushed *and* has a live PR, with `re-run` as the
  offered verb. Crash between `tp new` and the run insert → invariant 9's "no prior run" gate (a
  DB fact) re-cuts an existing worktree. → one rule: *record intent before the effect; reconcile
  the effect from the world* — adopt an existing PR for the branch instead of creating; derive
  worktree existence from `git worktree list`, not from run rows.
- **Tick-age and last-tick-error are Phase 1 requirements, not nice-to-haves** — they are the only
  signal separating "quiet" from "the loop died an hour ago", and `events` needs a tick-scoped row
  kind for gh/fetch failures. Rate-limit note: the poll costs 1 GraphQL point per call (~14% of
  quota at three repos) — latency, not API quota, is the wall; `max_agents` bounds neither of the
  two things that actually burst (CI jobs, the shared PR-stage pool).

### 7 · The trust boundary is not where §7 says it is — BOUNDARY · Phase-1-blocking

§7 asserts the push denylist "blocks the only push whose consequences are not confined to a
branch." Verified false in three independent ways:

- **The agent has its own push, pre-authorised.** `support-app/.claude/settings.json` is
  *tracked* (arrives in every worktree via git) and allows `Bash(git:*)`, `Bash(gh pr:*)`,
  `Bash(gh api:*)`; `.claude/settings.local.json` is in treepad's `[sync] include` (copied into
  every worktree the app cuts) and allows `Bash(node *)` among others. `Bash(gh pr:*)` includes
  `gh pr merge` — the app "never merges" while an agent it launched can. The doc never states what
  permission mode `claude -p` runs under. → invariant 17 gains: the app passes its own
  `--settings` file denying git/gh/network egress (the app owns pushing; the agent needs neither);
  drop `settings.local.json` from both `[sync]` lists.
- **The denylist is narrower than the Phase-1 executing surface.** On `support-app` alone, eight
  tracked paths outside the list execute inside required checks — `scripts/gen-graphql-prod.sh`
  (run directly by the required compat check), `scripts/gen.sh`, `eslint-ci.config.ts`,
  `vite.config.ts`, `pnpm-workspace.yaml` (which carries `patchedDependencies` — code injected
  into `node_modules` — and the registry the `.npmrc` token is sent to), `.npmrc`, `codegen.yml`,
  `next.config.js` — and `pnpm install`, the first step of all five checks, runs with
  `TIPTAP_PRO_TOKEN` in env. **`.mergify.yml` lives at the repo *root*, outside `.github/`, and
  is still not denylisted** — the file the app's own verdict is transcribed from, whose
  `label=bypass` + body-checkbox rule skips all checks, with the body half agent-controlled via
  the app's own `--fill`. And "package.json" is ambiguous: 6 tracked files match, two with
  postinstall hooks. → per-repo `[repo.deny]` glob list beside `[repo.checks]` (§8's blocking
  "only per-repo values" sentence is already gone — the doc won this and didn't take it); add
  `.mergify.yml`, `CODEOWNERS`, scripts, `.env*`; record the `.mergify.yml` blob SHA beside the
  predicate and refuse a green verdict when it moves (converts silent staleness into a row —
  same move as the base SHA).
- **No filesystem boundary.** Worktrees are siblings of `plain/.claude/` — from any worktree,
  `../.claude/command-centre.db` (including `launches`, the consent record), every run log with
  every composed prompt, the seam files, and *other agents' worktrees* are same-uid writable.
  → move the DB and logs out of sibling reach (`~/Library/Application Support/…`, 0700); treat
  the DB as untrusted-adjacent for the consent row.
- **Invariant 18 is one clause short** (unchanged verbatim from rev 2 despite the review): a
  request with *no* `Origin` (plain navigation, `<img>`, form GET) passes "rejects mismatched
  Origin", and `launch` makes this CSRF-to-code-execution. → destructive verbs POST-only; missing
  `Origin` rejected; require `Sec-Fetch-Site: same-origin`.
- **Consent is unbound from content.** The preview authorises ticket IDs; the prompt is composed
  hours later from files agents can reach. → store `prompt_hash` on the `launches` row at
  authorisation (the preview already composes it); step 10 refuses to spawn on mismatch →
  `seam changed`. One column, one `if`; the preview becomes a commitment.
- Free adjacent fixes: §13.3's `.env` paragraph is inverted (the synced secret-bearing file is
  `.env.development.local` — *in* the include list, contrary to the doc; bare `.env` doesn't exist
  in either repo) — delete the `.env` lines and rotate the tracked `.npmrc` token to the
  `${TIPTAP_PRO_TOKEN}` interpolation CI already uses. Require an `events` row on denylist
  refusals, Origin rejections and `--force` removals.

### 8 · The schema, written down at last — SCHEMA · Phase-1-blocking

The v2 review asked for the columns; revision 3 names none, and its one schema *claim* is false:
**`launches` is not "the one table not re-derived"** — `runs` holds at least seven non-derivable
facts (pgid, start time, baseline SHA, prompt hash, pushed tip, pushed-at, recorded base SHA),
which §8's own crash-recovery paragraph depends on. Settle before code:

- **A push-attempt entity** owning `(pushed_tip, base_ref, base_sha_at_push, pushed_at)`. Phase 1
  will hang these off `runs`; Phase 2 has a push with no run (`refresh`) and two runs per push
  (`re-run`) — that is a data-model inversion, not `ALTER TABLE`. `pushed_at` is also what
  `checking`'s still-unnumbered bounded wait needs.
- **`launch_members(launch_id, task_id)`** as a join table, authorised = "member of any active
  launch". `tasks.launch_id` (which §5's wording implies) makes every answer to the
  add-to-a-launch open question a migration.
- **Run disposition as data** (`kind`, `outcome`) rather than a branch in step 4 — the v2 fix that
  makes deferred local-verify additive, asked for twice now, adopted in neither revision.
- **Status as a derived label over stored facts** (unlocked, authorised, run outcome, verdict,
  base kind/ref/SHA), computed by one pure function the page and tests share. That makes the
  blocked/queued/ready cross an actual 2×2, lets `queued` say *why* (waiting-on-base vs
  waiting-on-slot — hours-or-forever vs seconds, the distinction the split was invented for), and
  gives orthogonal flags (`seam changed` — currently in the verb list but absent from the state
  table — draft-ness, `quarantined`) somewhere to live that isn't the enum. Widen `unlock`'s
  return now to carry the reason — the page requirement ("four queued rows behind one failed
  blocker must read as one problem") is underivable from `{unlocked, base_ref}`.
- **`ticket_url` as task identity with upsert** — re-running `to-tickets` must not mint new rows,
  or invariant 9 loses its key and double-launches (and per fix 9 there is no Activity-file
  backstop). Plus a schema-version row that refuses to start on mismatch.
- State-machine completeness, all cheap: `push failed` needs `retry push` (no agent);
  denylist-`needs you` needs a non-agent verb; `kill` off `checking` (the agent is dead by
  construction there); a state for a failed `tp new`; a `base gone`/`blocker withdrawn` terminal
  for dependents of a closed parent (a row that has run must never return to `blocked` — the
  current §3.8 transition contradicts `blocked`'s definition and drops consent); a `close PR`
  verb (the app can open a PR and has no sanctioned way to unopen one); and one normative copy of
  the state list (the PRD's table already diverges from §5 by a whole state).

### 9 · Finish the deletions — BOUNDARY · Phase-1, and it *shrinks* the design

- **Delete the Activity file.** Verified: every treepad reader — the launch guard, `tp ui`'s keys,
  the run-state display, the log pager — is gated on **Batch/manifest membership**
  (`tui_batch.go:26-28,136-176`, `tui_update.go:369-371`), and revision 3 writes no manifests. The
  file the app writes is read by nobody; invariant 6's three-clause rule, the §8 restart sentence
  that contradicts it (a fail-open contradiction *inside the document*), and the reimplementation
  of `launcher.ActivityPath` + `internal/slug` across a repo boundary are all paid for a guard
  with no reader. §15's "Fixed, and halved" should read "dissolved" — revision 3's own move,
  applied one step further. (If the interlock is ever wanted back, the trigger is "the app starts
  writing manifests", and `batch/` — a top-level *exported* package, so "separate repos make it
  structural" is false as stated — can export a three-line `ActivityPath`.)
- **Get worktree paths from `git worktree list --porcelain`,** not from reimplementing treepad's
  path formula (`lifecycle.go:50` — slug fold, prefix, sibling dir; `tp new` has no JSON output).
  That takes the hand-mirrored treepad contract surface from five (two commands + path + activity
  path + slug) to the two commands §1 claims.
- **Acknowledge what `link` bought before banning it** (inv. 5). GitHub Stacks exist *only* via
  the `gh stack` CLI (treepad ADR 0003) — `gh pr create --base` produces an ordinary PR with no
  stack navigator. The design says merge order is essential and publishes it to `127.0.0.1` on
  the author's laptop — the one surface the person merging cannot see. Either call
  `gh stack link` on app-owned branch pairs after both PRs exist (must-fix 4's actual defect was
  treepad's *unscoped caller*, not the mechanism — the app is scoped by construction), or put
  "Merge after #N" in the PR body via `--body`. Localhost is not an answer.
- Also delete from the doc: the `design-an-interface` repoint in §10 (nothing in the installed
  `to-plan` references it — verified grep), and restate the `to-tickets` residual honestly: the
  real change is *deleting* its manifest-emission step and the manifest reference file, not
  swapping a chain rule for edges.

---

## The eight foundations probed — headline answers

| # | Foundation (handoff) | Holds? |
|---|---|---|
| 1 | Unlock on PR-open | **Decision sound; four unnamed breakages.** Red-parent launches (preview should show the parent's verdict beside the base — a line the human reads, not a governor); draft blockers count as OPEN; the open-then-closed gap; the 14-day auto-close cascade. All named in fixes 3–5. |
| 2 | Never repaired; `refresh` = merge | **Justification verified stronger than claimed** (squash server-enforced; descendant PR diffs stay clean under repeated refresh — three-dot diff, checked). But the verb is inert (no push path), aims at a ref the happy path deletes, lacks its ff-precondition and its conflict state. Fix 4. |
| 3 | `launches` the only non-derived table | **False** — `runs` is non-derivable too, and the consent row is neither protected from agents nor bound to the content authorised. Fixes 7–8. |
| 4 | Bases derived from `blocked_by` | **Holds for fan-out** (must-fix 5's dissolution is genuine). Fan-in: correct, no deadlock, diamonds serialise on merged-ness as intended. Breaks on: merged blockers (branch deleted), cross-repo edges (count rule can't see repo), drafts. Fix 3. |
| 5 | treepad surface = two commands | **Understated: contract surface is five** (path formula, activity path, slug). Fix 9 gets it to two honestly. `link`'s deletion loses reviewer-visible stack structure — decide where merge order is published. |
| 6 | Verdict = pure over (predicate, rollup) | **Boundary right, signature wrong** (~8 inputs), rollup needs an ACL + latest-per-name reduction, grammar needs negation, `absent_ok` unsound over a snapshot, retry-pending has no data source. Fixes 1–2. |
| 7 | blocked/queued/ready cross | **Right idea; store the facts, derive the label.** Phase 3 is then additive; as a stored enum, `queued`'s two meanings and the orthogonal flags force edits. Fix 8. |
| 8 | Phase 1 = two tickets off `origin/main` | **Wrong cut.** It proves none of the machinery Phase 2 depends on, while every deferred unknown is external behaviour testable *now* for free. Amend: ticket 2 stacks off ticket 1, `max_agents = 1` — one slice then exercises stacked bases, expiry, `base moved`, retarget, refresh, and the queue, without concurrency. |

## Pre-build spikes (hours, no Go)

1. **One hand-made two-PR stack in `support-app`**: merge the parent; record whether GitHub
   auto-retargets the child (`delete_branch_on_merge: true` is confirmed; the retarget is the one
   server behaviour the whole stack-teardown story rests on and nothing verifies); whether any
   check re-runs (expect none); what Mergify's protection check shows on the stacked child; then
   label the retargeted child `ready-to-merge` and watch the queue.
2. **`gh pr create --fill` vs the required Linear check.** If a `--fill`'d PR from an agent branch
   fails `verify / Linear issue is linked`, **100% of Phase 1 PRs land in `needs you`** and Phase 1
   never reaches its only meaningful exit criterion. The repo carries two incompatible branch-name
   conventions, and the workspace ships a skill whose whole purpose is unblocking this failure.
   One test PR answers it.
3. Confirm `Lint GitHub Actions / Lint` never appears in PR rollups (it is `on: push` +
   path-filtered), so `absent_ok`'s semantics are exercised deliberately, not by surprise.

## Dimension verdicts, one line each

- **Simplicity**: prose simpler than the system — the central call, "base"'s three meanings, and
  the tick's per-repo/global nesting (steps 7 and 10 are not per-repo; `max_agents` starves repo 2
  deterministically) all under-specified; restate the tick as observe → decide → act.
- **Maintainability**: the deletions are right; the replacements (`refresh`, expiry) are inert or
  unscoped as written; the predicate copy needs a staleness detector (hash `.mergify.yml`); the
  state list exists in two diverging copies.
- **Extensibility**: Phase 2 is a rewrite of unlock as scoped; with fixes 3, 4 and 8 it becomes
  additive. Cross-repo (Phase 3) needs edge classes now, at zero Phase-1 cost.
- **Security**: the weakest dimension — all five v2 security findings survive verbatim, and the
  real boundary (the agent's own tool grant) was never drawn. Fix 7.
- **Performance/resilience**: the tick's failure semantics and its measured duration are the two
  structural items; both are shape decisions (fix 6). GitHub API quota is *not* the wall — latency
  and the shared PR-stage pool are.
- **Modularity**: purify the decisions that are dangerous (launch eligibility, disposition,
  denylist) not just the ones that were easy; `internal/plan` + `internal/verdict` +
  `internal/gh` ACL, with treepad's own `api_test.go` purity guard copied across.
- **Operability/phasing**: no dry-run, no per-tick action cap, no undo map, no tick-error surface,
  for the one component whose actions are outward-facing PRs on team repos — `dry_run = true` is
  one boolean and doubles as the classifier's acceptance harness.

## Verified and holding — the design's earned wins

`support-app`'s §8 predicate transcription is complete and correct (the one thing revision 3
clearly got right by reading the file) · rejecting Mergify's merge-protection check as the verdict
is confirmed with live data (FAILURE on three all-green PRs) · squash server-enforced in both
repos — §4a's core premise is airtight · repeated `refresh` does **not** pollute descendant PR
diffs · the app genuinely owns pushing (zero `git push`/`gh pr create` across implement,
code-review, tdd — exact line cites check out) · the consumer story (§6) exact in all three parts
· §11's "no longer relied on" list and §13's strikethroughs all justified (one — the `link`
/`restack` veto — right conclusion, wrong stated reason: scoping saves the app, not
non-invocation) · `tp remove --force` really is ~5 upstream lines · the allowlist-of-named-checks
verdict model is right (live rollups carry 21–53 entries of Socket/Greptile/Vercel noise) ·
`gh pr edit --base` exists, so the retarget verb is available · treepad's `[sync]` `!` exclusions
exist, so the `.env` fix is one deleted line, exactly as the v2 review said.

## Consolidated FALSE / STALE claims (for revision 4's §16)

| Claim (doc location) | Verdict | Reality |
|---|---|---|
| §2/§3.2 `gh pr list` as written supplies PR state + CI + unlock input | FALSE | defaults `--state open --limit 30`; fields omit `headRefOid`/`baseRefOid`/`isDraft` |
| §1/§4a/§13.9/PRD "queue rebases onto main" | FALSE | `update_method: merge` both repos |
| §6 "consumers parked indefinitely, by design" | FALSE | 14-day auto-close both repos; `keep-open` label is the out |
| §15 must-fix 6 "Fixed — re-check = workflow re-dispatch" | FALSE | no gating workflow has `workflow_dispatch`; mechanism is `gh run rerun` |
| §8 "red with a retry pending is `checking`" | UNSUPPORTED | rollup exposes no attempt data; retry runs attach to `main` |
| §15 must-fix 2 "Fixed, and halved" (Activity file) | FALSE premise | every treepad reader is manifest-gated; rev 3 writes no manifests; nothing reads the file |
| §4b/§8 "`launches` is the one table not re-derived" | FALSE | `runs` holds ≥7 non-derivable columns per §8's own restart story |
| PRD "Phase 1: `base moved`/`refresh` not reachable" vs inv. 11 | CONTRADICTION | unscoped expiry fires on `origin/main` drift (130–187 moves/14d) |
| Inv. 16 MERGED-blocker base = the blocker's branch | FALSE | `deleteBranchOnMerge: true` both repos; the branch is gone |
| §3.6 "`pull_request: edited` which nothing listens for" | IMPRECISE | `linear-issue.yml` (a required check) listens in both repos; conclusion survives |
| §13.3 `.env` paragraph + "no denylist to apply" | FALSE (again) | `!` exclusions exist; `.env.development.local` IS synced; bare `.env` absent from both repos |
| §12 "no chain-depth threshold is asserted" | FALSE (again) | `treepad-manifest.md:56-57` |
| §7 denylist "blocks the only push not confined to a branch" | FALSE | agents hold pre-approved `Bash(git:*)`/`Bash(gh pr:*)` via tracked/synced settings |
| §1 "never imports `batch/` … structural" | FALSE as stated | `batch/` is exported; it is a rule, and calling it structural means nobody writes the test |
| §10 `to-plan` references `design-an-interface` | FALSE | no such reference in the installed skill |
| §6/§14 `schema.ts` = 15,569 lines | STALE | 16,141 today; claim holds |
| §7 "~72 jobs" per services push | STALE | 30–53 check contexts on live PRs |
| §11 citation `docs/commands.md`-adjacent paths | IMPRECISE | `restack.go:87` is `internal/treepad/restack.go`, not `batch/restack.go` cited beside it |

## Prioritised recommendations

1. Fixes **1–4** (the PR-list contract, the verdict model, unlock, the base lifecycle) — they are
   the pure core Phase 1 exists to prove, and every one is currently specified against inputs or
   rules that are wrong on live data.
2. Fixes **6–7** (fail-closed tick shape; the trust boundary) — the two that cannot be retrofitted:
   one is loop structure, the other is where the DB and the agent grants live.
3. Fixes **5, 8, 9** (Mergify posture + spikes; the schema; finishing the deletions) — spec
   afternoons that decide whether Phase 2 is `ALTER TABLE` or inversion.
4. Amend the Phase 1 cut (ticket 2 stacks, `max_agents = 1`) and adopt the three pre-build spikes
   as Phase 1 entry criteria.
5. Record the outcome as **revision 4 with a §16**, following the §14/§15 convention — including
   the FALSE-claims table above, since the trail of *why* conclusions changed is this project's
   best artefact.

**Do not add.** Same closing note as the v2 review, still true: every fix above is smaller than
the mechanism it corrects, and five of the nine are deletions or single flags. The temptation to
re-add repair machinery in response to fix 5 should be resisted — owning the retarget is one
`gh pr edit` call, not a return of `restack`.
