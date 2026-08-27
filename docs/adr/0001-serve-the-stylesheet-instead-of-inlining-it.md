# 1. Serve the stylesheet instead of inlining it

**Date:** 2026-08-26 · **Status:** accepted

**Supersedes:** the CSS decision in [`plans/operator-surface.md`](../../plans/operator-surface.md)
§ "CSS, and how much".

## Context

`page.tmpl` inlines `page.css` into a `<style>` block on every response. The operator-surface plan
chose this deliberately, for two stated reasons: it keeps the route table shorter, and it keeps the
whole render golden-testable in one file. Both held while the stylesheet was 84 lines of monospace
table rules under a stated cap of a hundred.

The operator surface now gets a real design system — Tailwind v4, the prototype's oklch token set in
an `@theme` block, light and dark. A compiled sheet is one to two orders of magnitude larger than
what the plan was reasoning about. Inlined, it lands in `testdata/page.golden.html` on every
regeneration, so a layout diff arrives buried under a stylesheet diff and stops being reviewable —
which destroys the exact property the plan was protecting. The `GET /assets/` route the plan wanted
to avoid also already exists: htmx and the SSE extension are served from it.

## Decision

Serve the compiled stylesheet from `/assets/app.css`, out of the same `go:embed`ed asset FS that
already serves htmx. The golden files carry markup only.

## Consequences

The binary stays single and offline — `go:embed` is unchanged, only the delivery differs. Golden
diffs become markup diffs, which is the property the original decision wanted. Page load costs one
extra request against 127.0.0.1, cached thereafter, and a theoretical flash before the sheet lands
that is not observable on loopback. A CSS regression is invisible to the golden files, which was
already true when they inlined an unread 84-line block. If the sheet ever shrinks back under a
hundred lines, this reverts cheaply; the premise, not the judgement, is what changed.
