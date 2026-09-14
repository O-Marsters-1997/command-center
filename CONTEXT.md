# Command Centre

A local control plane that drives a DAG of tickets to reviewable pull requests using agents, one
worktree per ticket. This glossary covers the language of the app and its operator surface. It is
a glossary only — mechanism lives in `docs/designs/command-centre-design.md`, product framing in
`docs/prds/`.

## Language

### The work

**Ticket**:
One unit of work the app is driving, identified by its URL. Rendered as one row.
_Avoid_: Task, row (that is how a ticket renders), issue

**Ref**:
The tracker's own identifier for a ticket, as the board renders it (`#CC-1`), and the link back to
the tracker. A ticket has exactly one ref.
_Avoid_: Ticket (a ref names the work, it is not the work), number, key, ID

**Feature**:
The tracker's own grouping of tickets, and the unit the board scopes to. A ticket belongs to at
most one feature; a ticket the tracker puts in two is a tracker problem the app refuses rather
than resolves.
_Avoid_: Group (that is how the board lays out a blocker and its waiters), epic, project,
milestone, slice

**Slice**:
The set of tickets a single authorisation covers.
_Avoid_: Batch, group (group means something else here), selection, feature

**Exhausted**:
A property of a slice: every ticket it covers has produced a pull request, so the loop has nothing
further to start from it. It says the agents are finished, not that you are — an exhausted slice is
typically still waiting on your review.
_Avoid_: Done, complete, finished (each reads as merged), drained

**Run**:
One agent process spawned against one ticket.
_Avoid_: Job, execution, attempt

**Group**:
A blocker ticket and the tickets waiting on it, as the board lays them out.
_Avoid_: Cluster, stack (stack means the git relationship, not the layout)

**Peer**:
Another ticket with an open branch based on `main`, which this one is tested against for conflict.
Peership is not a plan fact and implies no blocker. It covers every open main-based ticket,
whatever slice or feature authorised it.
_Avoid_: Sibling (implies shared blockers, which peership does not require), neighbour, rival

### State

**State**:
The one label derived for a ticket each tick, from `plan.Status`.
_Avoid_: Status (reserved for `plan.Status`, the function that derives it), phase

**Unattended**:
A property of a state: the loop advances it without you. Its opposite is a state parked until you
act. Rendered as a filled disc against a ring.
_Avoid_: Owned, mine, blocked-on-you

**Reason**:
The sentence `plan.Status` returns beside a state, naming the blocker where there is one.
_Avoid_: Why, message, description

**Verb**:
A mutating action a state offers, queued as an intent and applied by the loop on a later tick.
_Avoid_: Action, command, operation

**Intent**:
A queued verb that the loop has not yet consumed.

**Tick**:
One pass of the reconcile loop: observe, derive, act.

### The surface

**Board**:
The grouped table of tickets. Also `#board`, the element htmx swaps.
_Avoid_: Table, list, grid, dashboard

**Detail**:
The panel that expands under a selected row, carrying the run log and the flag prose.
_Avoid_: Drawer, expansion, panel

**Band**:
The row of analytics cards above the board, counted off the current tick.
_Avoid_: Stats, summary, header cards

**Pill**:
The inline badge carrying a ticket's state, its unattended glyph and, while a run is alive, a pulse.
_Avoid_: Badge, chip, tag, label

**Tone**:
The health band a state collapses to: done, live, wait, stop or idle. Many states share one tone.
The surface colours by tone, never by state.
_Avoid_: Colour, variant, severity, status

**Grammar**:
A reusable markup-and-style pattern with exactly one definition, rendered identically by Go and by
Solid. The pill, the board table, the segbar and the run-log line are each a grammar.
_Avoid_: Component (that is any Solid function), widget, partial (that is how a grammar is stored)

**Island**:
A Solid component compiled to a custom element, mounted where direct manipulation needs client
state. Never inside a swap target.
_Avoid_: Widget, component (that is any Solid function), micro-frontend

**Run phase**:
A span of a run's log, cut at each skill invocation. Always qualified — bare "phase" means a
product phase.
_Avoid_: Section, stage, step

**Spend**:
What a run cost. Accruing tokens while alive, dollars from the `result` event once ended.
_Avoid_: Cost (ambiguous between the live and settled figures), usage, price

## Relationships

- A **feature** contains zero or more **tickets**; a **ticket** belongs to at most one **feature**
- A **slice** authorises one or more **tickets**; a **ticket** may appear in many slices over time
- A **slice** is **exhausted** once every **ticket** it covers has a pull request
- A **ticket** has exactly one **state** per **tick**, and a **state** offers zero or more **verbs**
- A **verb** becomes an **intent**, which a later **tick** consumes
- A **state** has exactly one **tone**; a **tone** covers one or more **states**
- A **grammar** has one definition and many use sites, in templates and **islands** alike
- A **group** has one blocker **ticket** and zero or more waiting **tickets**
- A **ticket** has zero or more **peers**; peership crosses **slice** and **feature** alike
- A **ticket** has exactly one **ref**
- A **ticket** has zero or more **runs**; only the latest one renders
- A **run** produces a log of **run phases**, and one **spend** figure
- The **board** contains **groups**; a **detail** belongs to exactly one **ticket**
- An **island** is never a descendant of the **board**

## Example dialogue

> **Dev:** "Four rows went red at once. Is that four problems?"
>
> **Operator:** "Look at the **group**. One **ticket** is `failed` and three are `queued` behind it.
> That's one problem — the **reason** on each queued row names the blocker."
>
> **Dev:** "So do I press a **verb** on all four?"
>
> **Operator:** "Only the blocker. The other three are **unattended** — filled discs. The loop moves
> them the moment the blocker's PR opens. Ring means it's parked until you act, and only the failed
> one has a ring."
>
> **Dev:** "And the ring on the `merged` row at the bottom?"
>
> **Operator:** "Same rule. Merged looks finished but the loop will never clear it — you have to
> remove the worktree. It's yours."

## Flagged ambiguities

- **"worktree"** was used for two things: the prototype's third column header, which holds the
  ticket title, and `row.Worktree`, which is a filesystem path. Resolved: **worktree** is the path,
  always. The column showing the title is headed **ticket**, and its subordinate line is the
  **reason**.
- **"task"** was the glossary's word for a unit of work while the schema, the Go code and half the
  board called the same thing a **ticket**, leaving `Ticket` meaning the work in Go and the tracker
  reference in the UI. Resolved: **ticket** is the work, at every layer. The board's `#CC-1` column
  is headed **ref**. Task is gone.
- **"phase"** had two senses before this work and now has three: product phases in
  `prd-command-centre.md`, build-step phases in GitHub issues #2–#7, and a span of a run's log.
  Resolved: the log sense is always **run phase**, never bare "phase". The other two collision
  remains, and the operator-surface PRD's rule stands — name a phase, do not number it.
- **"cost"** was used for both the live token count and the settled dollar figure. Resolved: both
  are **spend**, which reads as tokens while a run is alive and as dollars once it ends.
- **class names in ADR 2** were listed as `.st`, `.tl` and `.seg-h`, none of which exist. The
  sheet has `.pill`, `.ticket-link` and `.segbar`. Resolved: the ADR names a **grammar**, not a
  class; the Tailwind migration removes the global class namespace entirely, so the ADR's
  collision guard becomes unnecessary rather than broken.
- **"group"** named three things at once: the board's layout of a blocker and the tickets waiting
  on it, the tracker's own grouping that the import page lists, and an internal key tying one
  authorisation's intents together. Resolved: **group** is the board layout, always. The tracker's
  grouping is a **feature**. The third is not a domain term and has no entry here.
- **"status"** was used for both a ticket's derived label and the Linear project status in the
  prototype. Resolved: **state** is the derived label; the prototype's project status has no
  referent in this app, which has no project concept.
