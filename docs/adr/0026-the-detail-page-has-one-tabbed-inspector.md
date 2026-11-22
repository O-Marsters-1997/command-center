# 26. The detail page has one tabbed Inspector

**Date:** 2026-11-22 · **Status:** accepted

## Context

A ticket's Detail page carried two right-hand surfaces: the stack panel (feature progress, This
ticket, In flight, PR, Checks, Spend, Edit blockers) and the topbar's Activity tray. Both were
popovers positioned over the content, so they overlapped each other, and neither could stay open
beside the transcript. The tray also never hid when closed, so the Activity button looked dead.

## Decision

**The Detail page has one right-hand column, the Inspector, with two tabs: Details and Activity.**
Details is the stack panel's content unchanged. Activity is the tray's body, the same fragment and
the same 5s poll.

- **Docked.** At 1180px and above the Inspector is a 300px column beside the transcript that
  pushes the content, the way the Sidebar does. Below 1180px it is an overlay opened on demand, as
  the stack panel is today.
- **Collapsible, and remembered.** Open or collapsed, and the active tab, live in a cookie the
  server reads, so the first paint is right and nothing is swapped to restore it (the rail keeps
  its expanded features the same way, [ADR 15](0015-pages-are-routes-beside-a-polling-rail.md)).
  A fresh visit opens on Details.
- **The topbar's Activity button opens the Inspector on the Activity tab** on a Detail page, and
  collapses it when that tab is already showing. A second control on the Inspector collapses it
  from either tab.
- **Detail pages only.** Every other page keeps the Activity tray as a dropdown from the same
  button, which must hide when closed. Feature, graph, board and Insights pages get no Inspector
  until they have content of their own for a Details tab.

One Inspector with a Details tab per page type was rejected for now: feature pages have no
Details content yet, so the tab would be empty. Rendering the Inspector client-side as an island
was rejected: the tabs and the collapse are cookie state and `hx-` markup, with nothing that needs
direct manipulation ([ADR 1](0001-one-global-stylesheet.md)).

## Consequences

The Activity tray is rendered in two places, the topbar dropdown and the Inspector's tab, from one
`trayBody` template and one `/activity/tray` poll target, so the two cannot drift. A Detail page
never shows the dropdown. `DESIGN.md`'s feature-stack-panel paragraph describes the Details tab
and gains the Inspector's tabs and collapse.
