# 27. Review comments travel to the agent as a run attachment

**Date:** 2026-11-22 · **Status:** accepted

## Context

Request changes quoted every pending review comment, with its diff hunk, into the follow-up prompt.
Hunks are long and the prompt is stored on the run row, hashed, and shown in the run's page. Ten
comments made a prompt the owner could not read back and the agent could not tell apart from the
owner's own words.

## Decision

- Pending review comments travel as a JSON file beside the run's other artifacts,
  `<RunsDir>/<run id>.review-comments.json`. Each entry holds `tip`, `path`, `line`, `side`, `hunk`
  and `body`.
- The prompt names that absolute path and carries one standard paragraph telling the agent to
  address every entry. It no longer contains a hunk.
- Request changes queues the owner's note as the follow-up's message and the comments as a separate
  field of the intent payload, still deleting the comments in the same transaction.
- `plan.FollowUpPrompt.Compose` is the only place a follow-up prompt is composed. The run id is
  unknown until the run row exists, so the loop composes with `plan.RunIDPlaceholder` in the path
  and substitutes the id when it spawns.
- The loop is the only writer of the attachment ([ADR 9](0009-the-loop-owns-reconciled-state.md)).
  It writes the file before it records the prompt.

## Consequences

This reverses local-review-and-raise's "the prompt quotes each comment". A follow-up with no
comments composes exactly as it did, so CI, verify and owner follow-ups are unchanged. The prompt
header "Changes requested on this branch." appears only when comments are attached, so a note-only
request changes reads as an owner follow-up.
