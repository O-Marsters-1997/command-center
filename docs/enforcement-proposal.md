# Making the standards hold

_Proposal, 2026-08-24. Third of three. Reads on top of `docs/enforcement-inventory.md`
(what we claim) and `docs/agent-failure-taxonomy.md` (what actually broke)._

The thesis I am working to: the rules need to apply uniformly, and there has to be
a cheap way to tighten them for one repo or a handful without forcing that
tightening on every other repo. One `go-idiomatic` skill is the right idea and the
wrong delivery mechanism, because a skill is a single document and different Go
projects need different rules.

My answer in one line: uniformity is a property of where the check fires, not of
where the rules live, so put the hook in one global place and the rules in each
repo.

## The measurement that should change the plan

Pass 1 said this repo has no hooks. That was true of the repo and wrong about the
setup. There is already a global one: `~/.claude/hooks/check-staged-comments.sh`,
a `PreToolUse` Bash hook that blocks `git commit` whenever the staged diff adds
comment lines, points at `comments.md` as the single source, and tells the agent
to run `clean-comments`. It is well built. It fires in every repo with no
per-repo wiring. It exits 2, so it genuinely blocks.

It went live on 23 August at 19:31. Of the 164 doc comments on unexported
identifiers in this repo, **49 were introduced after that moment**, across 12
separate commits, every one of which added comment lines to Go files and
therefore tripped the hook.

So the hook fired, the agent ran the judgement pass, and 49 violations of the one
clause of the standard that is trivially countable went in anyway.

That is the most useful result in all three documents, because it rules out the
easy explanations. The wiring is not the problem. The hook is global, blocking,
correctly placed before the commit, and points at a single-source standard which
the skill it delegates to reads rather than paraphrases. Every piece of advice in
the literature about placement was already followed here.

What failed is the check itself. The hook detects "you added a comment" and then
hands the actual decision to a model. It is an inferential control wearing a
computational control's clothes, and it inherits the failure rate of the
judgement it delegates to. Roughly 100%, on this clause, over two days.

There is a second, quieter problem. `clean-comments` documents the escape hatch
in its own workflow: trimming a long comment counts as adding lines, so a
legitimate cleanup pass trips the hook, and the skill tells you to commit with
`CC_COMMENTS_REVIEWED=1`. The hook's author already patched one loophole (an
unchanged retry used to pass) and wrote a comment about not wanting the block to
degrade into a nudge. The documented workflow makes the override routine, which
gets to the same place by a different route.

**The rule I would take from this: a hook must carry the verdict, not a request
for one.** Name the rule, name the line, give the corrected form. If the check
cannot do that, it does not belong in a hook.

## The design

Three tiers. Each has one home and one enforcer, and nothing appears in two
tiers.

| Tier | Contains | Lives in | Enforced by | Changes when |
| --- | --- | --- | --- | --- |
| 1. Universal | Rules true in every Go repo I own | One shared `golangci-base.yml` and one shared ast-grep rule pack, next to `comments.md` in the dotfiles | Global `PostToolUse` hook | Rarely, and every repo feels it |
| 2. Repo-local | This repo's own conventions and any tightening of tier 1 | `.golangci.yml` and `.agents/sg-rules/` in the repo | Same global hook, which picks them up automatically | Freely, per repo, no coordination |
| 3. Judgement | What no rule can express | `go-idiomatic`, shrunk to only these | The model | When the standard itself changes |

The delivery mechanism is one hook script, installed once in
`~/.claude/settings.json`, which on every Go file write runs four things against
the edited package: the shared linter config, the repo's own linter config if it
has one, the shared ast-grep pack, and the repo's own ast-grep pack if it has
one. A repo that adds nothing gets tier 1 and behaves uniformly. A repo that
wants more drops a file in and gets it, with no change anywhere else.

That is the whole answer to the thesis. Uniformity comes from the hook being
global and unconditional. Specificity comes from the rules being local and
additive. Neither requires the other to move.

### Why this beats putting tiers in the skill

The obvious alternative is a profile system inside `go-idiomatic`: a base
standard, a `.go-standards.toml` per repo naming a tier, and the skill reading it.
I would not do this. It is model-mediated, which means it inherits exactly the
failure the 49 comments measured. Adding configuration to a judgement layer makes
the judgement more complicated without making it more reliable.

### The drift problem solves itself

`.agents/skills/` in this repo is 39 real directories, not symlinks to a central
copy, so each repo will eventually hold its own `go-idiomatic` and they will
diverge. Right now only this repo has one, so this is a risk rather than a fact.

Mechanising the rules mostly removes it for free. Of the 107 rules, 70 have a
ceiling below rung 6. Once those live in the shared packs, prose drift stops
mattering for them, because the prose is no longer what enforces them. The drift
that remains covers the 35 judgement rules, which is a small enough surface to
maintain by hand.

## What I measured before proposing any of this

**Hook latency, the number the literature does not have.** golangci-lint with
this repo's config, warm cache: **0.22s for one package**, 0.84s cold, 2.1s for
the whole repo. ast-grep with a rule pack: **0.019s for the whole repo**, 0.010s
for one file. A `PostToolUse` hook running the linter on the edited package plus
both ast-grep packs costs about a quarter of a second. The lit review's advice is
to keep synchronous hooks to single-digit seconds; this is an order of magnitude
inside that, which means the hook can be synchronous and blocking rather than
async and advisory.

**ast-grep composes shared and local rule packs natively.** `sgconfig.yml` takes
a list of `ruleDirs`, so one shared directory plus one repo directory is a
two-line config and a single scan. I ran it with both and it works. This is the
strongest practical argument for ast-grep over Semgrep, and it is not about
matching power. Semgrep is not installed here and is a Python process, so I
cannot give it a latency number; I would need to measure before making a claim.
But ast-grep is a single binary at 19ms with native pack composition, and that
settles the question the lit review left open for the hook use case.

**The top rule works as a structural rule.** I wrote the "doc comment on an
unexported declaration" rule and it finds 149 of the 164 my Python heuristic
found. The gap is entirely `var` and `const` declarations, which I left out of the
prototype. So the biggest category in pass 2 is genuinely enforceable at rung 3.

**Rule authoring costs more than it looks.** The counterweight to all of that
enthusiasm. Writing the trivial half of a rule, "find `exec.Command`", took seven
attempts. `exec.Command($$$)` silently matches nothing; `exec.Command($A, $$$)`
matches 19 of the 29 that `grep` finds. It fails quietly and undercounts, which
is the worst failure mode a check can have, because a rule that finds 19 of 29
reads exactly like a rule that found everything.

Meanwhile `noctx` found all 29 with one config line and no authoring at all.

That comparison sets the priority order for everything below: **exhaust the
off-the-shelf linters before writing a single structural rule, and give every
structural rule a test file with known-bad and known-good cases before trusting
its count.** ast-grep has `ast-grep test` for exactly this, and Semgrep's
methodology in the lit review says the same thing. I did not follow it for the
prototype and that is why I nearly reported 19 as 29.

## The proposals, ranked by impact over cost

Impact is the count of measured violations from pass 2 that the check would have
caught. Cost is my honest estimate of setup time.

### 1. Make the comments hook decide instead of asking

Impact: **49 measured violations**, and it is the only item here that fixes a
proven failure rather than a projected one. Cost: an afternoon.

Add the ast-grep doc-comment rule (finished, with `var` and `const`, and a test
file) to the existing hook. On a hit, print the rule name and the offending
lines, not a request to go and think about it. Keep the blunt "you added
comments" check as a second, separate message, because it catches things the rule
cannot.

Then deal with the override. It exists because a cleanup pass legitimately trips
the line-counting check. Once the doc-comment rule is exact, the exact rule does
not need an override, and only the blunt check does. Splitting them lets the
countable clause block without an escape hatch while the judgement clause keeps
one.

### 2. Shared linter base plus the global PostToolUse hook

Impact: **111 measured violations** (24 argument-limit, 29 subprocess context, 25
unwrapped errors, 9 globals, 8 sleeps, 7 helpers, 5 parallel, 3 prealloc, 1
range), plus nine zero-violation linters that start preventing the next class of
mistake. Cost: one config file and one hook script, half a day.

This is the best value in the document. Every one of those 111 needs no rule
authoring, and pass 2 showed the leak rate is flat at 0.3 to 1.1 per 100 lines
across two days, so the leak continues at that rate until something fires on the
write.

golangci-lint has no config inheritance, only `-c PATH`. I would not build a
config merger for it. Run it twice, once with the shared base and once with the
repo's own. Two runs is 0.44s and needs no code.

Two sequencing constraints from the literature, both of which bite here. Ship
advisory first and promote to blocking once the count is zero, because nothing
gets enabled over a dirty tree. And scope `noctx` to non-test code before it goes
anywhere near a hook: 25 of its 56 hits are tests calling their own `httptest`
server, a 45% false positive rate against Google's under-10% tolerance, and a
compliant agent acts on a false positive instead of ignoring it.

### 3. The repo-local rule pack convention

Impact: not measurable yet, and I want to be straight about that. The
repo-specific rules do not exist, so there is no count. Cost: a directory
convention and one line in the hook.

This is the piece the thesis actually asks for, and its value is what it makes
cheap later. Tightening a rule for one repo becomes "add a YAML file to
`.agents/sg-rules/`" rather than "edit the shared skill and hope the change does
not leak". The first two candidates from pass 2 are both repo-specific rather
than universal: every subprocess in a tick takes the tick's context, and the
render path and the reconcile path derive through one function rather than each
computing the verdict separately.

Build this third, not first. It is the mechanism, and a mechanism with no rules in
it proves nothing.

### 4. Shrink `go-idiomatic` to the 35 judgement rules

Impact: no violations caught, which is why it is fourth. Cost: an hour of
deletion.

2,400 lines across seven files, and 70 of the rules in it are about to be
enforced by something that does not need prose. A standard written as a check
costs no context until it fires; the same standard written in a skill costs
context every time the skill triggers. The skill's own first section already says
"don't write rules here for anything the linter already enforces", so this is
following its own instruction.

Delete the rule, keep the reasoning where the reasoning is the point, and let the
check's message carry the rest.

### 5. A coverage floor, advisory

Impact: unmeasurable and possibly large. Cost: three lines in CI.

Nothing in this repo measures coverage anywhere. The testing reference states
four coverage targets and checks none of them, and a coverage floor is the only
defence anyone in the literature names against agents deleting tests. I have no
baseline, so this ships as a printed number with no threshold until there are two
weeks of readings to set one from.

### 6. Flip `default-signifies-exhaustive` to false

Impact: unknown, cheap to find out. Cost: one line and however long the cleanup
takes.

`exhaustive` is one of only eight enabled linters and 30 of the 37 switches in
non-test `internal/` carry a `default:`, so it currently exempts most of what it
looks at. Flip it, count what appears, then decide. This is a measurement dressed
as a proposal.

## What I would not do

**A tier system inside the skill.** Covered above. Configuration on top of
judgement.

**Semgrep, for the hook.** Not installed, Python startup, and ast-grep already
provides the pack composition that was the actual requirement. I would still
reach for Semgrep for a one-off audit where authoring comfort matters more than
latency.

**A config merger for golangci-lint.** Running it twice costs 0.22 seconds.

**Generating the skill prose from the rule packs.** Tempting, and it would make
drift structurally impossible. It is also the kind of clever that someone has to
decode at 3am when the generator breaks.

**A `PreToolUse` deny on Go writes.** Wrong control for style. A blocked agent
with no fix path retries badly, and `PostToolUse` with an exact message gives it
the fix path.

## Sequencing

Week one: item 1, because it fixes a measured failure and the prototype already
exists. Then item 2 advisory, with `noctx` scoped to non-test code.

Week two: fix the 111. Argument-limit first, since 24 functions over the limit is
the failure that compounds and `derive` at 10 parameters is the worst of them.
Promote each linter to blocking as its count reaches zero.

Week three: item 3, with the two repo-specific rules pass 2 turned up as its first
content. Then item 4, deleting from the skill everything now held by a check.

Items 5 and 6 are measurements and can happen at any point.

## What I still cannot answer

The two most interesting failures in pass 2 are the two with the least evidence.
Duplicated derivation across the render and reconcile paths rests on a single
instance, and nothing mechanical will ever catch it. Table-driven tests being the
exception rather than the default (36 `t.Run` calls against 217 test functions)
is measurable but not fixable by a rule, and my honest read is that the rule
should be softened to match reality rather than enforced, because a table-driven
test with one row is worse than a plain one.

Both need the free-text pass extended from 5 PRs to 20, read against the design
doc rather than against the linters. That is the session where `code-review`'s
spec axis earns its place, and none of the six proposals above should wait for it.
