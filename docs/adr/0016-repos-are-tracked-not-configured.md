# 16. Repos are tracked, not configured

**Date:** 2026-10-08 · **Status:** accepted

## Context

Per-repo settings (`stacking`, `deny`, `checks`, `compat_check`, `mergify_sha`, `verify_command`,
`tracker`) lived in the `[[repo]]` blocks of the app's own config. A change to what agents may touch
or which checks gate a merge was a change to a file outside the target repo, reviewed by no one who
owns that repo, and read once at startup.

## Decision

A repo's settings live in `.command-centre.toml` at the root of the target repo and are read from its
`origin/main`, never from a ticket's branch or worktree.

- `[[repo]]` keeps `name`, `remote` and `path`. Any per-repo key left in a block is a startup error
  naming the key and pointing to the file.
- Settings are an observed fact. The observer reads them after its fetch and records them in
  `plan.Observation`, as a per-repo map plus any read error. `plan.RulesFor` builds `Rules` from the
  daemon's keys and the observation, so the loop and the board derive with the same settings, and a
  change on `origin/main` takes effect on the next tick with no restart.
- A missing file answers defaults. A parse error, an unknown key or an invalid predicate is a read
  error: it is recorded as the tick's last error and the repo is skipped for every act step that
  tick.
- The file's own path is always denied to pushes, whatever the repo's own `deny` says, so an agent
  cannot loosen the policy that constrains it.

## Consequences

Tightening a repo's guardrails is a pull request to that repo. Loosening one is too, and the file is
denied to agents, so only a human can merge it. A repo that has not yet merged its file runs on
defaults, which means no verdict and only the built-in deny list.
