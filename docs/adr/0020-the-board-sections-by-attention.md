# 20. The board sections by attention, retiring the group layout

**Date:** 2026-10-09 · **Status:** accepted · **Amends:** ADR 7

## Context

The board lays rows out in groups: a blocker with its waiters indented by `data-depth`. Signal
lists everything by attention, and the owner's first product principle is that what needs them
outranks what is merely running. A blocker that needs the owner and its blocked waiters cannot sit
in one nested group and in two attention sections at once.

## Decision

- The board renders sections in a fixed order: Needs you, In progress, Ready, Blocked, Done, keyed
  by `plan.Glyph` (ADR 18).
- Dependencies are words, not indentation: a blocked row names its blockers, and every row names
  what it unlocks. The dependency shape lives in the feature graph.
- Scoping still admits a blocker chain whole (ADR 7) before rows are sectioned, so an out-of-scope
  blocker still renders.
- The group layout, the `data-depth` indent rules and the `group-head` border are deleted.

## Consequences

**Group** leaves the glossary at cut-over. The board needs an inverse of `BlockedBy` per render to
say what a row unlocks. Reading a deep chain means opening the graph.
