# 3. Seams are removed

**Date:** 2026-08-27 · **Status:** accepted

## Context

A seam was named prompt text describing an interface that did not exist yet, pasted into the prompt a
launch authorised and retired once its producers merged. It was built for a second repo that never
arrived: no config ever used `[[seam]]`, and the code was threaded through config, `cc.Task`,
`plan.Task`, the loop, preview, launch and a board column.

## Decision

Delete it: `[[seam]]`, `seams` on `[[task]]`, `cc.Seam`, `plan.SeamCheck`, `plan.SeamChanged`,
`composePrompt`, the `seam changed` column and the `seams` column.

`plan.Compose` returns `/implement <ticket url>`. The prompt hash still binds consent to content,
since the ticket body can change between authorising and spawning. `OpensAsDraft` turns on a gating
edge alone, so a cross-repo blocker still opens a draft PR.

## Consequences

Launch and preview lose their "no readable content" refusal. Nothing observable changes for a config
with no seams. To rebuild, see `internal/plan/seam.go` and `internal/cc/seams.go` at `bea7062`.
