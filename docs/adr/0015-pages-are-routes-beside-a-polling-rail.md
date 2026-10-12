# 15. Pages are path routes, swapped beside a polling rail

**Date:** 2026-10-09 · **Status:** accepted

## Context

A session rail sits beside every page: Needs you, In flight by feature, Settled. A rail re-rendered on
every click or poll would lose its scroll, its expanded features and focus. Every page, including the
launch dialog, should be linkable.

## Decision

- Rendering is Go `html/template` and htmx. The graph and launch islands are the only client-rendered
  parts.
- Every page is a path: `/s/{owner}/{name}/{n}`, `/f/{feature}`, `/f/{feature}/graph`,
  `/f/{feature}/launch`, `/launch`, `/tickets`, `/insights`, `/repos`, `/repos/{owner}/{name}`.
- One handler per page renders the full layout. Rail links fetch it with `hx-select="#main"` and
  `hx-push-url`, so navigation swaps `#main` and never the rail.
- The rail polls `GET /rail` inside a scroll container that does not swap. Expanded features are
  `<details>` whose open set lives in a cookie the server reads, the selected session comes from the
  current URL, and elapsed timers tick client-side from a `data-started` attribute.

## Consequences

The rail costs one fragment render per poll. SSE is limited to the run log; pushing the rail per tick
is an option if polls lag.
