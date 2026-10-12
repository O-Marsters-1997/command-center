# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

One developer, the owner. They scope work into GitHub tickets and hand the tickets to coding
agents. Mostly they work at a desk. They also check in from a phone to see what is running and to
unblock agents: answer a question, resolve, re-run, review, or arm a launch for when its blockers
merge. Some tickets on the board belong to other humans; the "yours" split exists for that, not
for a team.

## Product Purpose

Command Centre launches coding agents on scoped tickets and watches each one through to a merged
PR. Tickets are grouped into features, and a feature carries a dependency stack. Launching a
feature therefore means many agents running in waves, as each blocker merges. Success is the
owner keeping several features moving across repos at once without losing track of which agent
needs them.

## Positioning

Other parallel-agent tools treat sessions as independent chats. Command Centre knows the
dependency stack. It can say which ticket unlocks which, launch tickets the moment their blockers
merge, and show a whole feature's wave progress across repos.

## Operating Context

- On a heavy day: 2–3 repos' worth of work, 2–4 features running, under 10 live agents.
- Agents run in one git worktree per ticket. They open draft PRs and wait on required checks. The
  repos are squash-merge only, so GitHub's PR state is the merge test.
- Usage is bounded by five-hour and weekly model limits, shown as meters. Launching pauses when
  the five-hour limit is spent.
- It is reached at the desk and from a phone over a tailnet through Caddy, with a login. No page
  makes a third-party request.
- The stack is one Go binary (`cmd/cc`). Go `html/template` pages are driven by htmx, with two
  Solid islands (the dependency graph and the launch modal) and Tailwind v4 CSS-first. See
  `AGENTS.md` § Frontend.

## Capabilities and Constraints

- **States.** `plan` derives a ticket's state from observation. The surface knows each state only
  through one of eight glyph words: blocked, ready, running, pending, checking, attention, done
  and failed. Attention covers review me, needs you, awaiting input and conflict resolved. Failed
  covers every state a human must repair, such as failed runs, failing checks, conflicts with
  main and a gone base.
- **Talking to an agent.** An agent can ask the owner a question mid-run. The ticket then awaits
  input and holds its worktree. The rail card shows the question with Reply. A reply or any
  follow-up resumes the agent's own session, and the session reads as one transcript across
  turns.
- **Verbs:** launch (now, or armed to launch on unlock), kill, re-run, retry push, cancel,
  refresh, abort, resolve, commit resolution, follow-up, and remove worktree. Verbs queue intents;
  the loop is the only writer of reconciled state.
- **Destinations:** the session view (transcript, raw log, feature stack), the feature graph and
  board, the launch dialog, all tickets, insights (limits, spend per merged ticket), and the
  tracked repos and repo pages. Every page is a path route beside a polling rail.
- **Terminology** follows `CONTEXT.md`. There is no project concept: where the design shows a
  project, the repo stands in.
- **Not built yet**, shown in the design as target UI: the composer's model, effort and worktree
  pickers; Undo on every verb; the peek panel; the merged-tickets table on Insights; and graph
  nodes that open their session.

## Evidence on Hand

- The live data is the owner's own tracked repos and tickets. `internal/demo` simulates a fleet
  for screenshots, and `e2e/` drives the binary against a fake `gh` and `tp`.
- The design reference is `design/signal/`, a static prototype vendored from
  `command-center-design`. Only its `acme` billing feature comes from real fixtures. Its other
  projects, logs, insights numbers and extra repos are illustrative and must stay labelled as
  such outside the prototype.

## UI Direction

The goal is to converge every page on **Signal**, the design system in `DESIGN.md`, using
`design/signal/` as the visual reference. The functionality of the prototype already exists. The
remaining work is visual parity, in this order: shared foundations (primitives, glyph ring,
tokens, icons), the shell and rail, the session and composer, the feature board and graph, the
launch dialog, then tickets, insights and repos, the keyboard sheet and toasts, and finally a
phone pass. A page is done when its PR shows it beside the matching prototype route at 1440px and
390px, and an `/impeccable audit` against `DESIGN.md` finds no P0 or P1 issues. Prototype
elements that need new data or behaviour become their own tickets and do not hold a page back.

## Product Principles

1. Attention first: what needs the owner outranks what is merely running.
2. The feature is the unit of launch; the ticket run is the unit of work.
3. Dependencies are shown, never hidden. Blocked work says what it waits on.
4. Every unblocking action works from a phone.
5. Quiet while agents work; loud only on state changes that need a human.
