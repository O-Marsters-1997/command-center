# 4. Seams are removed

**Date:** 2026-08-27 · **Status:** accepted

## Context

A seam was a named piece of prompt text describing an interface that did not exist yet. A
`[[task]]` listed the seams it touched; `composePrompt` read each one from
`<root>/.claude/seams/<name>` and pasted it into the prompt a launch authorised. A `[[seam]]`
block could name `producers` and `lands_at`, and once every producer's PR had merged the seam
retired itself: the paste came from the real file at `lands_at`, read out of `origin/main`,
instead of the hand-written stand-in. Editing a seam after authorisation changed the composed
hash, which held the row queued and flagged `seam changed` on the board rather than spawning
against text the operator had not consented to.

It was built for a second repo that has not arrived. `[[seam]]` has never appeared in a config,
`.claude/seams/` has never held a file, and every seam test constructs its own fixture. The
feature could not have carried a real second repo anyway: `NewObserver` keys branches globally
rather than by `(repo, branch)`, so two repos sharing a branch name collide in `obs.PRs`,
`obs.Worktrees`, `obs.BranchTips` and `obs.MidMerge`. Cross-repo prompt text was never the
blocker.

The cost was spread thin and everywhere. Two config types, a `Seams` column, a `Seams` field on
both `cc.Task` and `plan.Task`, a resolution function threaded through the loop, the verb path,
the preview and the launch route, a refusal path at every one of those call sites, and a board
column. `plans/repo-and-ticket-model.md` moves all of that code in later phases, so it goes
first and the later phases move less.

## Decision

Seams are deleted. `[[seam]]`, `seams` on `[[task]]`, `cc.Seam`, `plan.SeamCheck`,
`plan.SeamChanged`, `retirementsByName`, `allProducersMerged`, `composePrompt` and the
`seam changed` column all go, along with the `seams` column on `tasks`.

`plan.Compose` loses its content parameter and returns `/implement <ticket url>`. A prompt is
that line plus the ticket's body, which is what the spawn path already appended. The prompt hash
still binds consent to content. The body can change between authorising and spawning, and a
mismatch still refuses.

`OpensAsDraft` now turns on a gating edge alone. A task with a cross-repo blocker opens its PR
as a draft exactly as before; nothing else did.

## Consequences

The launch and preview routes lose their "no readable content" refusal, because composition can
no longer fail. `NewServer` drops two parameters and the `Server` struct drops three fields,
`repoPaths` among them. That was the last thing in the server resolving a path from a workspace
root, which is what phase 3 goes after.

Nothing observable changes for the live config. It configures no seams, so every row composes
the same prompt it composed yesterday and every stored hash still matches.

If a second repo arrives and wants shared prompt text again, the shape to rebuild is in this
repo's history: `internal/plan/seam.go` and `internal/cc/seams.go` at commit `bea7062`, with
their tests. Fix the branch keying in `observe.go` first. Without that a second repo does not work
at all, seams or no seams.
