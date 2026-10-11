# 23. A project is a set of repos

**Date:** 2026-10-11 · **Status:** accepted

## Context

The Signal session rail groups work by more than one repo, and the operator wants to narrow the
rail and the All tickets board to a set of repos at once. The natural word is project. The glossary
used to ban it: the tracker's label prefix `project:<slug>` marks a feature, so "project" meant
feature.

## Decision

**Project** is the app's word for a named set of repos. A repo belongs to at most one project.

- The tracker label prefix `project:<slug>` still marks a feature. It is an outside string that
  fleet, `to-tickets` and the tracker decoder read, and the app does not rename it. The glossary's
  Feature entry says so.
- Nothing infers a project. Not a shared owner, not a label, not a path. A project exists only
  because the operator grouped repos on the Repos page.
- Projects are configuration, written directly by the web handlers (ADR 11), and a project scope
  narrows what renders, never what the loop reconciles (ADR 7).

## Consequences

"Project" no longer means feature anywhere in the app, but it still does in the tracker's labels, so
code that reads a label says feature and code that reads a repo grouping says project. Deleting a
project loosens its repos and never deletes them. Because nothing derives a project, there is no
rule to keep in step with GitHub, and `plan` gains nothing (ADR 15).
