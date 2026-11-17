# 24. A pull request opens on raise in a manual repo

**Date:** 2026-11-17 · **Status:** accepted

## Context

When a workflow finished, the loop pushed the branch and opened a draft PR straight away. The PR
was the only place to read the diff and leave comments, so review happened on GitHub. In my own
repos that is fine. In a shared repo, an overnight run with nobody watching fills it with draft PRs
that no human has looked at.

Unlock already treats an open PR, draft included, as enough
([design §4](../designs/command-centre-design.md#4--unlock)), and that rule stays.

## Decision

**A repo can make opening a pull request wait for a human.** `.command-centre.toml` takes
`raise = "manual" | "auto"`, default `"auto"`. Anyone can review locally in any repo. Only a manual
repo waits for the verb.

- A ticket in a manual repo whose workflow has finished, has a local tip and has no PR reads
  `awaiting PR`, under the attention glyph. Its verbs are raise, follow-up and re-run.
- **Raise binds to the SHA you reviewed.** The intent carries `(ticket, sha)`, and the push goes
  ahead only while the local tip still equals that SHA. Any newer commit voids it. This is the same
  move as the launch's prompt hash.
- Raise gates the first push only. Once the PR is open, refresh, resolve and follow-up tips push as
  they do today. The draft gate still decides between draft and ready.
- Raise queues an intent and nudges the loop, as the import does
  ([ADR 8](0008-a-feature-is-closed-under-blocked-by.md)). The handler waits until the tick that
  consumes the intent has run, then returns the PR or the push error. The loop stays the only
  writer ([ADR 9](0009-the-loop-owns-reconciled-state.md)), and the push is Go: v1's `pr` step
  has already written the body by then.
- The changes tab, `/s/{owner}/{name}/{n}/changes`, renders `git diff origin/<base>...<tip>` in
  Go templates, with htmx comment forms on each line. `internal/web` runs read-only git per render
  and caches nothing.
- Draft comments are stored with the tip they were written on. Request changes deletes them and
  queues one follow-up whose payload quotes each comment with its file, line and hunk. The
  follow-up resumes the latest agent step that can edit, not the latest agent step, which in v1
  is the read-only `pr` step.

A manual repo means a blocked ticket waits for your raise of its blocker. Stacking on an unraised
local branch was rejected, because base-moved and refresh would then have to compare local tips,
and rejecting a blocker would leave every descendant built on withdrawn work. A per-launch toggle
was rejected because whether a stray PR is a problem depends on who watches the repo, not on the
launch. Pushing from the handler was rejected because it would race `Pushable` and put git writes
in `internal/web`. Comments kept across rounds and marked outdated were rejected, because nothing
reads them after the follow-up has carried them ([ADR 20](0020-history-is-kept-only-where-something-reads-it.md)).

## Consequences

An overnight run in a manual repo finishes the unblocked tickets and then waits for you. The
morning review is what unblocks the next layer. The changes page has no syntax highlighting,
because highlighting would mean a new Go dependency and new colour tokens.
