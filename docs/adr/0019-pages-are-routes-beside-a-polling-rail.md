# 19. Pages are path routes, swapped beside a polling rail

**Date:** 2026-10-09 · **Status:** accepted

## Context

The new UI puts a session rail beside every page: Needs you, In flight by feature, Settled. Today
every page is a query string on `/` (`?sel=`, `?feature=`, `?repo=`, `?view=graph`), the launch
dialog has no GET address, and the board polls itself by swapping its own `outerHTML`. A rail
re-rendered on every click or poll would lose its scroll, its expanded features and focus. The
prototype is a client-side SPA, built for speed, not as a recommendation.

## Decision

- Rendering stays Go `html/template` and htmx. The graph and launch islands stay islands.
- Every page is a path: `/s/{owner}/{name}/{n}`, `/f/{feature}`, `/f/{feature}/graph`,
  `/f/{feature}/launch`, `/launch`, `/tickets`, `/insights`, `/repos`, `/repos/{owner}/{name}`. Old
  query-string URLs answer 301. POST and JSON endpoints keep their paths.
- One handler per page renders the full layout. Rail links fetch it with `hx-select="#main"` and
  `hx-push-url`, so navigation swaps `#main` and never the rail.
- The rail polls `GET /rail` inside a scroll container that does not swap. Expanded features are
  `<details>` whose open set lives in a cookie the server reads, the selected session comes from the
  current URL, and elapsed timers tick client-side from a `data-started` attribute.

## Consequences

Every page is linkable, including the launch dialog. The rail costs one fragment render per poll.
SSE stays limited to the run log; pushing the rail per tick is a later option if polls lag.
