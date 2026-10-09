# 18. Glyph replaces tone as the only colour key

**Date:** 2026-10-09 · **Status:** accepted · **Supersedes:** the tone half of ADR 1

## Context

The surface colours by `plan.Tone`, which collapses 22 states into five words (done, live, wait,
stop, idle). Signal draws eight status glyphs and reserves indigo for "your move". `ReviewMe` tones
`wait`, alongside `Blocked` and `Queued`, so tone cannot colour it indigo without recolouring the
states it shares a band with.

## Decision

- `plan.Glyph(State)` returns one of eight words: `blocked`, `ready`, `running`, `pending`,
  `checking`, `attention`, `done`, `failed`. It is pure, like `Tone` was, and `Tone` is deleted.
- The mapping is a table in `plan`, with `attention` covering `ReviewMe`, `NeedsYou` and
  `ConflictResolved`, and `failed` covering every state a human must repair.
- The rail group is a function of the glyph: `failed` and `attention` are Needs you, `done` is
  Settled, everything else is In flight.
- The `pill` class family becomes `glyph` (`glyph glyph-{word}`), and `[data-tone]` becomes
  `[data-glyph]`. Go still returns a word, never a utility string.

## Consequences

A new state needs one row in one table. `pill_test.go` is replaced by `glyph_test.go`, which pins
the eight words and the class grammar. Segbars and meters colour by glyph too, so a review-ready
segment is indigo everywhere.
