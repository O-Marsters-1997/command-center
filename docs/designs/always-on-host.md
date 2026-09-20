# The always-on host

**Date:** 2026-09-20 · **Revision 1** · **Status:** sketch, not built

Follows from `cloud-agents.md` §8 item 4: a small always-on host gets the overnight goal on its
own, no design change, no gate lost. The cloud run kind is still the right direction after this
host exists, not a fallback for when it runs out of room. Local stays what actually runs tickets
today, and worktrees keep earning their place regardless: a bad run is still fixed by provisioning
one and prompting against it directly, which is exactly what `cloud-agents.md` §5 means by
"repairs are always local," whatever kind produced the branch. Cloud is worth building because of
what it is, not because local ran out of capacity.

This document covers infrastructure only. Nothing in `internal/cc` or `internal/tracker` changes.
The DAG, authorisation, the tick, the deny gate and both existing run kinds stay exactly as they
are; only where the process lives moves.

---

## 1 · What this is

`App.Run` already blocks forever, running the reconcile loop and the board's HTTP server in two
goroutines until it gets a signal. It just does that on a laptop today, so the tick loop dies the
moment the lid closes. This plan moves that same process to a Hetzner VM that stays up, and
handles everything a laptop currently gives it for free: a login session, a network connection, a
Postgres instance, someone noticing when it dies.

Local runs stay local runs. The worktree, the `claude -p` spawn and the deny gate all run the same
code, just on the VM instead of under a desk. Nothing here is the cloud run kind from
`cloud-agents.md`, and `runs.kind` gets no new value out of this work.

## 2 · The VM

Hetzner, sized for `max_agents = 1` or `2`. That is the number of `claude -p` processes and
worktree checkouts the box needs to carry at once, not a guess at eventual scale. Raise it once a
real night's run says the cap is the constraint, which is also the trigger for revisiting the
cloud run kind (§7).

## 3 · State: a managed Postgres

`internal/cc/store.go` already opens Postgres by DSN (`database_url` / `CC_DATABASE_URL`), so
nothing in the app cares where that instance lives. Point it at a small managed Postgres (Neon or
Supabase's free tier fits this load) instead of running `compose.yaml`'s container on the VM
itself. Backups and patching become the provider's job. The only cost is one more account to
secure, and that account already needs securing under the self-hosted option too, just without the
backups that come with it.

## 4 · Process supervision: systemd

`App.Run` has no fork, no PID file, no restart-on-crash of its own, there's no separate
`cc-daemon` binary. A `.service` unit with `Restart=on-failure`, enabled at boot, covers exactly
the gap: the tick should survive its own panics and a VM reboot the same way it's meant to survive
a closed laptop. No app code changes for this.

## 5 · Access: SSH tunnel, no bind change

`app.go` binds the board to `127.0.0.1` on purpose. Invariant 18 in `command-centre-design.md`
lists that alongside POST-only mutating verbs and Origin/Host checks. Moving the process to a VM
doesn't change that: reach the board with `ssh -L <port>:localhost:<port> vm` when you want to
look at it, and close the tunnel after. No new bind address, no auth layer bolted on to compensate
for one.

## 6 · Credentials: one-time interactive login

`gh` and `claude` each hold their own session, outside this app's config. `internal/cc/runner.go`
strips `ANTHROPIC_API_KEY` from every spawned agent on purpose (inv. 17: no run inherits the app's
own key), so an env var on the VM doesn't authenticate the agent processes; only a logged-in
`claude` CLI session does. SSH in once during provisioning, run `gh auth login` and `claude login`
interactively, and let both cache their credentials on disk. Rebuilding the VM means doing this
again; that's an accepted cost of skipping a credential-management system this app has no other
use for.

## 7 · Monitoring: a health check that pings you

Nothing here watches itself. A cron job hitting the board's local port (or checking the systemd
unit's status) every few minutes, alerting on failure, is what stands between a dead tick and
finding out cold some morning. This is small on purpose: one script, one alert channel, no metrics
stack for a single-operator overnight tool.

## 8 · What the cloud run kind is actually for

Not overflow capacity. The case for it holds even if this VM never fills up:

- **Sandboxing.** A hosted provider's agent runs in its own throwaway environment by
  construction. A local run shares this VM with the orchestrator and every other worktree on it,
  contained only by the deny gate and whatever the agent's own tool permissions allow.
- **Introspection.** Cursor already has a UI for watching an agent work and reading back what it
  did. `cloud-agents.md` §5 scopes a cloud row's own detail panel as thin on purpose, "links out to
  the provider," because building a second one here would duplicate what Cursor already gives you.
- **Cost.** Cursor's implementation agents run cheaper than a `claude -p` subprocess against this
  app's own settings file, for the kind of small, well-scoped ticket this system is built to
  produce.
- **A thinner control plane.** Every ticket a cloud provider executes is a `claude -p` process and
  a worktree checkout this VM never has to carry. §4's division of labor already says the control
  plane should never execute anything; today it does anyway, for every local run. Moving execution
  off it is that rule catching up with the code.

None of this is built. The `Runner` interface is still pgid-shaped (§1), `disposeRun` still
depends on a worktree existing (`cloud-agents.md` §8 issue #3), and the deny gate degrading to a
flag instead of a refusal on a cloud row (`cloud-agents.md` §8 issue #1) is a real cost of this
direction, not a footnote. It's the next design to write, not something this VM's growth is meant
to force.

## 9 · Left open

- **VM rebuild playbook.** Losing the box means redoing the `gh`/`claude` logins by hand (§6) and
  repointing `database_url` if the managed Postgres also moves. Neither is written down anywhere
  but this document yet.
- **Disk growth.** Worktree checkouts accumulate on the VM the same way they do on a laptop, and
  nothing here adds cleanup beyond whatever the app already does.
- **No restore drill.** Moving to a managed Postgres removes the backup problem but nobody has
  actually pulled a restore from it yet, so the guarantee is the provider's SLA, not a tested one.
