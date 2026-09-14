# Plan: repo and ticket model — phase 8

> Source: the grilling session of 2026-08-27 on branch `cc-repo-model-location-independence`, which
> settled the design. Supersedes `docs/designs/location-independence.md` and
> `docs/adr/0003-a-repo-is-a-remote-not-a-path.md`, both written before the session and both stale.
> Origin: `.claude/handoffs/command-center__remodel-repos-for-location-independence.md`.

## What already landed

Phases 1 to 7 shipped. Seams went in ADR 4. goose owns the schema from `0001_init.sql`. `CC_DATA_DIR`
plus a clone is enough to run the app, and a `[[repo]]` block names a remote rather than a path.
`tasks` is `tickets` at every layer, `internal/tracker` reads GitHub issues, and `GET /import`
brings a `project:` group into the table. `[[task]]` has left the config.

Phase 8 is what is left: issues #128 and #129.

## What phase 8 needs from the earlier design

**The route.** `POST /ticket` edits one ticket's app-owned fields. Body: `ticket=<url>`, plus
`branch` and `blocked_by`. It writes an intent row and redirects, like every other write handler,
and goes behind `requireBrowserOrigin` as `POST /launch` and `POST /verb` already do.

There is still no `POST /tickets` and no per-ticket create endpoint. Labelling an issue and
importing its group is how a ticket enters the fleet.

**The migration.** `0004_withdrawal.sql` is `ALTER TABLE tickets ADD COLUMN withdrawn_at TEXT`.

**`tickets` after this phase:**

| column | owner | notes |
|---|---|---|
| `url` | tracker | primary key, the issue URL |
| `source` | derived | `github`; the adapter that fetched it |
| `repo` | config | which `[[repo]]` block, matched on the URL's owner/name |
| `group_key` | import | the `project:` label it came in under |
| `title`, `body`, `status` | tracker | refreshed on every import |
| `synced_at` | import | when the tracker last answered for this row |
| `branch` | app | generated `cc-<number>-<slug>`, editable |
| `blocked_by` | app | JSON array of URLs, seeded from GitHub dependencies, editable |
| `withdrawn_at` | app | set when an import no longer returns it |

---

## Phase 8: Withdrawal and editing

**Covers**: retracting a ticket without opening sqlite3, and correcting what the tracker cannot
tell you.

### What to build

Migration `0004_withdrawal.sql` adds `withdrawn_at`. An import that no longer returns a row
previously in its group sets it, which is what moving an issue to `status:backlog` or closing it
now does. The loop and the page read only rows where it is null, so runs, pushes and events stay
attached and no foreign key ever fires. A later import returning the row again clears it.

`POST /ticket` edits `branch` and `blocked_by` on the page, writing an intent row the next tick
applies. Editing a branch on a ticket that already has a worktree is refused rather than applied,
since the worktree and the row would disagree.

### Acceptance criteria

- [ ] Relabelling an issue to `status:backlog` and re-importing withdraws it, and its run history survives
- [ ] A withdrawn ticket disappears from the board and stops being reconciled
- [ ] Relabelling back and re-importing restores it with its history intact
- [ ] Editing blockers on the page changes the unlock decision on the next tick
- [ ] Editing the branch of a ticket that already has a worktree is refused, naming the worktree
- [ ] Nothing in the codebase deletes a row from `tickets`
