# 26. Feature and ticket pages share one tabbed right sidebar

**Date:** 2026-11-22 · **Status:** accepted

## Context

The ticket page's feature-stack panel and the Activity tray were two independent popovers. On a wide
window they overlapped: the stack panel is docked at 300px and the tray floats from the top bar over
the same corner. A feature page had no details panel at all.

## Decision

- Feature (board and graph) and ticket pages render one right sidebar with two views, Details and
  Activity. Details is the feature-stack panel on a ticket and the feature's project, ticket and
  wave counts and status counts on a feature page. Activity is the tray's body.
- The tabs are native radio inputs and the collapse control a native checkbox, styled with `:has()`.
  No script and no island: the choice is presentation state that nothing server-side reads.
- Pages that use the sidebar render the top bar without the Activity dropdown. Every other page
  (Insights, Tickets, Repos, History and so on) keeps the dropdown.
- Below 1180px the sidebar is still the `popover` overlay the stack panel used, opened by the same
  toggle; the collapse control only exists above it.
- The tray fragment refreshes both the dropdown button and the tab label out of band; whichever the
  page lacks logs a missing-target event from htmx and is otherwise ignored.
- The Activity panel keeps `#activity-tray-body`, its 5s poll and its out-of-band count refresh, so the
  undo form and `/activity/tray` are unchanged. The poll runs while the Activity tab is hidden because
  the tab label carries the in-progress count.
- htmx attributes, `#main`, `#board` and the rail's scroll container are untouched
  ([ADR 15](0015-pages-are-routes-beside-a-polling-rail.md)). A navigation swaps `#main`, which holds
  the sidebar, so the selected tab and collapsed state reset to Details and open.

## Consequences

Tab and collapse state is not remembered across navigation. If that matters it moves to a cookie the
server reads, as the rail's open set does. The Tray view model is needed by the sidebar and by the
dropdown, so both read `.Tray`.
