# 14. Glyph is the only colour key, and the board sections by attention

**Date:** 2026-10-09 · **Status:** accepted

## Context

Signal draws eight status glyphs and reserves indigo for "your move". The owner's first product
principle is that what needs them outranks what is merely running, so the surface is ordered by
attention, not by dependency.

## Decision

- `plan.Glyph(State)` returns one of eight words: `blocked`, `ready`, `running`, `pending`,
  `checking`, `attention`, `done`, `failed`. The mapping is one table in `plan`, with `attention`
  covering `ReviewMe`, `NeedsYou` and `ConflictResolved`, and `failed` covering every state a human
  must repair.
- The template composes `glyph glyph-{word}` and `[data-glyph]`. Go returns a word, never a utility
  string. Segbars and meters colour by glyph too.
- The rail groups by glyph: `failed` and `attention` are Needs you, `done` is Settled, everything else
  is In flight.
- The board renders sections in a fixed order, Needs you, In progress, Ready, Blocked, Done, keyed by
  glyph. Scoping admits a blocker chain whole ([ADR 6](0006-scope-is-a-view-concern.md)) before rows
  are sectioned.
- Dependencies are words, not indentation: a blocked row names its blockers, and every row names what
  it unlocks. The dependency shape lives in the feature graph.

## Consequences

A new state needs one row in one table. A review-ready segment is indigo everywhere. The board needs
an inverse of `BlockedBy` per render to say what a row unlocks, and reading a deep chain means opening
the graph.
