# 22. Import is per repo

**Date:** 2026-11-16 · **Status:** accepted

## Context

An import intent named only a feature, and the loop read that feature's label from every ready
repo. A label shared by two repos could not be imported from one of them, and the repo page could
not say what an Import button would pull in.

## Decision

- An import intent carries the repo it imports from in its payload. The loop reads only that
  repo's tracker and withdraws only that repo's tickets the tracker no longer returns.
- An empty payload keeps the old meaning, every ready repo, for `cc import <feature>` and the
  launch dialog's feature import.
- The Projects page offers the import on each ready repo's card, sized by the tickets the tracker
  holds for it.
- A feature may span repos: importing it from a second repo adds tickets and withdraws none of the
  first repo's.

## Consequences

`store.ImportRepoTickets` scopes withdrawal by repo, and the closure check of ADR 8 still runs over
the whole feature after each import. A blocker that sits in a repo not yet imported refuses the
import until that repo is imported too.
