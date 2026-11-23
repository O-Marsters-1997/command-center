# 28. Charts get one data hue

**Date:** 2026-11-23 · **Status:** accepted

## Context

Signal says a colour is a state, a project's hue or your next move, and forbids gradients,
ticket cards and shadows in the page flow. The Insights page plots trends over time. Drawn in state
colours the marks would read as states; drawn in indigo they would read as your move.

## Decision

Insights charts take five deviations from Signal, and only they do:

- One violet data hue, `--color-viz`, with `--color-viz-soft` for fills.
- Gradient area fills under a line.
- Sparklines.
- Green and red change chips, reusing the existing `s-done` and `s-failed`.
- Bordered 14px cards (`--radius-card`) with no shadow.

Scope is chart marks and the Insights cards that hold them. Violet charts data only, never state
or action: indigo still means your move, and state colours keep their meaning. `--color-viz` is
the only new colour token.

## Consequences

Still forbidden: gradients, sparklines, `viz` and 14px cards anywhere outside Insights charts;
shadows on any card; ticket cards in lists; a second data hue. DESIGN.md's frontmatter and
Don't/Do lists carry the same rules. This relaxes
[ADR 14](0014-glyph-is-the-only-colour-key.md) only for chart marks.
