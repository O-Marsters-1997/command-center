# 23. Protected paths push and are shown loudly

**Date:** 2026-11-17 · **Status:** accepted

## Context

The push policy had one tier. Any diff that touched a policy path was refused, and the refusal
recorded only the first matching path. The page showed the row as `needs you` and gave no reason.
Neither re-run nor retry-push could clear it, because the next run's diff matched the same rule.

Ticket twenty#2 deletes the Node server, so it has to delete the server's workflows under
`.github/**` and edit the root `package.json` and `bun.lock`. That work can only be pushed by hand,
outside cc. The defaults were hardcoded in `plan.defaultDeny`, and a repo's `deny` list could add to
them but never remove one.

## Decision

**The push policy has two tiers. A denied path refuses the push. A protected path pushes normally
and is shown loudly.** Protected paths add no hold, no approval and no new state. They only change
what the page and the PR show.

- Default deny: `.command-centre.toml`, `.mergify.yml`, `.env*` and `.npmrc`. These are the
  agent's own policy, the verdict predicate's source, and secrets or registry tokens. A deny
  refusal records every matching path and the rule each one hit, and the session page shows them
  as its reason.
- Default protect: `.github/**`, `CODEOWNERS`, `**/package.json`, the lockfiles and
  `pnpm-workspace.yaml`.
- `.command-centre.toml` takes `deny` and `protect` lists. `deny` adds patterns. A `protect`
  pattern that equals a default deny pattern moves it to protect for that repo, except
  `.command-centre.toml` itself, which stays denied. A path that matches both lists is denied.
- The loop classifies each changed path at push time and records the protected ones, with each
  one's rule and line counts, on the push row. `plan` derives the highlight from the latest push,
  so `internal/web` runs read-only git at most, never a write.
- A ticket whose latest push has protected paths shows a `protected` flag on its board row and in
  the rail. The session page lists each path with its rule and counts, linked to the PR's diff. The
  PR body gets a "Protected paths" section and a `cc-protected` label.

A held push with an approve verb was rejected. It would have added a new state and a stored
approval for a review the PR already offers. A deny override per ticket was rejected because it
puts a decision about policy on a launch form.

## Consequences

Install-time code in `package.json` and workflow edits now reach CI as a draft PR without a human
first reading them. Review in the app and on GitHub is the control instead. Design §7's claim that
no diff touching a policy path is ever pushed now holds for denied paths only. A repo that wants the
old behaviour lists the protected defaults in its own `deny`.
