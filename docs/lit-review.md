# Enforcing coding-agent standards deterministically

_Literature review. 20 sources, reviewed 2026-08-24._

## Scope

The question is how to make coding agents work inside standards that hold. Two halves to that: picking standards that are worth enforcing, and enforcing them with something other than the model's goodwill. The sources split cleanly along that seam. Half of them are twenty years of static analysis and architectural governance practice, written for humans, and they transfer almost intact. The other half are agent-era pieces about hooks, skills and harnesses, and they are mostly rediscovering the first half with a new audience.

## Sources reviewed

**Static analysis in practice (pre-agent, and the strongest evidence base here).**
Sadowski et al., [Lessons from Building Static Analysis Tools at Google](https://cacm.acm.org/research/lessons-from-building-static-analysis-tools-at-google/) (CACM 2018) is the anchor paper: field data on false positive tolerance, workflow placement, and what makes engineers act on a finding. Its predecessor, [Tricorder: Building a Program Analysis Ecosystem](https://alastairreid.github.io/RelatedWork/papers/sadowski:icse:2015/) (ICSE 2015), gives the platform design and the contributor accountability model. [Software Engineering at Google ch. 22](https://abseil.io/resources/swe-book/html/ch22.html) covers what happens when a standard changes and existing code has to move.

**Architectural governance.** Ford, Parsons and Kua, [Building Evolutionary Architectures](https://nealford.com/books/buildingevolutionaryarchitectures.html), supplies the fitness function vocabulary. Brian Perry, [Architecture that enforces itself](https://brianperry.dev/posts/2026/architecture-that-enforces-itself/), is the only source that applies it directly to agents, and the only one with the remediation-message idea.

**Rule authoring and structural matching.** Semgrep's [rule writing methodology](https://semgrep.dev/blog/2020/writing-semgrep-rules-a-methodology/) and Trail of Bits' [advanced Semgrep guide](https://appsec.guide/docs/static-analysis/semgrep/advanced/) cover process and technique respectively. [ast-grep](https://ast-grep.github.io/) and [JSSG](https://codemod.com/blog/jssg) are the alternative rule and codemod engines. Alexis King, [Parse, don't validate](https://lexi-lambda.github.io/blog/2019/11/05/parse-don-t-validate/), is the case for enforcement that needs no rule at all.

**The agent control layer.** The [Claude Code hooks reference](https://code.claude.com/docs/en/hooks) is the mechanism; Konishi's [complete guide to Claude Code hooks](https://hidekazu-konishi.com/entry/claude_code_hooks_complete_guide.html) is the practitioner's read, and the source of most of the footguns below. Anthropic's [skill authoring best practices](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/best-practices) and the [skill-creator SKILL.md](https://github.com/anthropics/skills/blob/main/skills/skill-creator/SKILL.md) cover the other half of the configuration surface.

**Harness engineering.** Fowler's [Harness Engineering](https://martinfowler.com/articles/harness-engineering.html) provides the guides-and-sensors taxonomy. HumanLayer's [Skill Issue](https://www.humanlayer.dev/blog/skill-issue-harness-engineering-for-coding-agents) is the opinionated practitioner counterpart. Fowler's [Anchoring to a reference application](https://martinfowler.com/articles/exploring-gen-ai/anchoring-to-reference.html) covers the few-shot alternative to rules.

**Evaluation.** Hamel Husain's [evals FAQ](https://hamel.dev/blog/posts/evals-faq/) supplies the error analysis method and the deterministic-before-judge hierarchy. Shankar et al., [Who Validates the Validators?](https://arxiv.org/abs/2404.12272) (arXiv 2404.12272), contributes criteria drift and little else applicable here.

**Industry practice.** CodeScene's [agentic AI coding best practices](https://codescene.com/blog/agentic-ai-coding-best-practice-patterns-for-speed-with-quality) is vendor content, so discount accordingly, but it is the only source with numbers on agent performance against code health and the only one that names test deletion as a failure mode.

A note on access: the CACM page returns 403 to automated fetches. Its content here comes from the [Google Research listing](https://research.google/pubs/lessons-from-building-static-analysis-tools-at-google/), a [summary of the paper](https://blog.dornea.nu/notes/lessons-from-building-static-analysis-tools-at-google/), and search results carrying the specific figures. Treat the quoted percentages as second-hand.

## The mental model

1. **Effective false positive rate.** Google's definition: a false positive is any report the developer did not want to see, regardless of whether the tool was technically correct. The tool author does not get a vote. This is the single most useful reframe in the whole reading list, and it is the number that kills rule sets.
2. **Fitness function.** From Building Evolutionary Architectures: an objective test of one architectural characteristic. They split atomic (one characteristic) versus holistic (several interacting), and triggered (runs on an event) versus continual (always on). A dependency rule, a coverage floor and a p99 latency assertion are all fitness functions.
3. **Guides and sensors.** Fowler's harness engineering article splits controls into feedforward (guides, which prevent the bad output) and feedback (sensors, which catch it after the fact). Each can be computational (linter, type check, test) or inferential (an LLM reviewing). Four quadrants, and the computational ones are the cheap reliable ones.
4. **Progressive disclosure.** Skills load metadata always, body on trigger, bundled files on demand. It matters here because a standard written into a skill costs context every time it triggers, whereas the same standard written as a check costs nothing until it fires.
5. **Parse, don't validate.** Encode the constraint in the type so the illegal state cannot be built, instead of checking for it in scattered places. The cheapest enforcement is the one with no rule to maintain.
6. **Structural matching.** ast-grep and Semgrep match on the syntax tree rather than characters. This is what lets a repo-specific convention become a real check instead of a fragile grep.
7. **Criteria drift.** From the EvalGen paper: people cannot write good grading criteria before seeing outputs, and the criteria they write change once they do. Applies directly to standards. Your list of rules is a hypothesis until agent output tests it.
8. **The deterministic layer.** In Claude Code that is permissions plus hooks. A `PreToolUse` deny is evaluated before permission-mode checks, so it holds even under `bypassPermissions`. CLAUDE.md is advice; a hook is a guarantee.

## Consensus view

**Advice in a document is not enforcement.** Every agent-era source in this list says it, and the pre-agent sources implied it. Brian Perry puts it best: human developers pick up conventions by osmosis and fear of code review, and agents have neither. Google reached the same place for humans decades earlier, having watched a bug dashboard nobody visited fail completely.

**Put the check where the work happens.** Google's central analysis dashboard failed. Moving the same analyses into code review, on by default, produced tens of thousands of daily fixes that engineers made voluntarily. The agent equivalent is a hook firing on the write, not a CI job discovered twenty minutes later. CodeScene's version is three checkpoints: during generation, pre-commit, and PR pre-flight.

**A fix beats a finding.** Google's preferred report format is one with a suggested fix that applies with a click. Semgrep has `fix` and autofix. Perry writes error messages specifically to inject remediation into the agent's context, and calls it a bargain at one extra string. For agents this is stronger than for humans, because an agent that reads a good error message closes the loop unsupervised.

**Simple checks dominate.** Google's high-impact results came from analyses with no dataflow or pointer analysis at all. This is the permission you need to write blunt structural rules rather than building an analysis framework.

**Deterministic before inferential, always.** Hamel's eval hierarchy is assertions and regex first, then reference-based checks, then LLM judges, because judges need 100+ labelled examples plus ongoing alignment work. Fowler's anchoring piece reaches the same conclusion from the other side: for anything a codemod can do, use the codemod, and keep the model for the drift that needs judgement. Harness engineering names the same split as computational versus inferential controls.

**Nothing gets enabled before the codebase is clean.** Google's compiler-error checks permanently ratchet quality up, but only after a team has fixed every existing violation across the codebase. Perry runs new rules as warnings before promoting them to blocking. Ignore this and every agent turn burns tokens on pre-existing noise.

**Rules earn their place by failing first.** Perry calls the good ones fossils, written after a real incident rather than derived from an architecture document. HumanLayer says bias towards shipping and only tune the config after real failures. Anthropic's skill guidance says the same thing in eval form: run the task without the skill, document what actually broke, then write the minimum that fixes it.

## Tensions and trade-offs

**How much false positive tolerance does an agent have?** Google's numbers are human numbers: under 10% effective false positives for review-time checks, with the real figure just under 5%. Nobody in this reading list has measured the agent equivalent, and the two directions are not obviously the same. An agent never gets annoyed and never clicks "not useful". But it is compliant, so a false positive does not get ignored, it gets acted on, and you end up with a wrong change plus wasted turns. My read is that agent tolerance is lower than human tolerance, not higher, and you should treat 10% as a ceiling you are nowhere near rather than a target.

**Block or advise.** A `PreToolUse` deny is absolute and cannot be argued with, which is exactly right for secrets and destructive commands. It is wrong for style, because a blocked agent retries, and a blocked agent with no fix path retries badly. Google's answer for humans is that only checks worth failing the build should fail the build. There is no clean rule here and you will have to make the call per check.

**Rules up front or rules from failures.** Ford's method is to name your architectural characteristics and write fitness functions for them. Perry and HumanLayer say wait for the failure. Criteria drift says the second camp is right about the mechanism, since you genuinely cannot write the good version of a rule before seeing what the agent does. But some rules are not negotiable and should exist on day one, before any incident has proved them necessary.

**MCP or CLI.** Fowler's reference-application pattern serves compilable code samples through an MCP server. HumanLayer says skip MCP for anything with good CLI coverage, because CLIs compose with grep and jq and cost far less context. Both are right about their own case, but the general lean should be towards CLI.

**Skills or checks.** Anthropic's degrees-of-freedom framing is the sharpest tool for this decision. Fragile, consistency-critical, one correct sequence, so low freedom, so a script. Many valid approaches, context-dependent, so high freedom, so instructions. A standard that can be stated precisely enough to belong in a skill can usually be stated precisely enough to be a check, and then it belongs in the check.

## Practical patterns

**The enforcement ladder.** For each standard, take the highest rung that can express it:

1. Make the violation unrepresentable in the type or schema. No rule to maintain, no false positives, fails at compile time.
2. Use an existing formatter, linter or type checker, and surface it through a `PostToolUse` hook so the agent sees the error on the same turn instead of at PR time.
3. Write a structural rule in ast-grep or Semgrep for repo-specific conventions that no off-the-shelf linter knows about.
4. Write a fitness function in CI for the properties that only exist across files: dependency direction, layer boundaries, coverage floors.
5. Use `PreToolUse` deny for the things that must never happen, and only those.
6. Use model judgement for what is left, and expect to maintain it like a product.

**Writing a check, using Semgrep's methodology.** Start from a concrete known-bad snippet, not an abstract rule. Build a test file holding both the bad and the good version. Write the pattern, then narrow with `pattern-not` and widen with `pattern-either`. Run it against real repos and count both misses and noise. Only then wire it into CI. This process is what separates a rule that survives from a rule someone disables in a month.

**The remediation message is the interface.** For a human, the message explains. For an agent, it is the entire fix instruction, arriving in context at the exact moment the agent can act on it. Name the rule, name the violation, and give the corrected form. Add an autofix where the transform is mechanical.

**Plan, validate, execute.** Anthropic's pattern for high-stakes agent work: have the agent write its intent to a structured file, validate that file with a script, and only then execute. It moves the checkpoint before the damage rather than after, which is the same reason Google moved analysis from post-commit to review time.

**Error analysis to choose the standards.** Hamel's method applied to agent output. Collect 20 to 30 real agent PRs, open-code what went wrong in free text, then group into a taxonomy, then keep going until new transcripts stop producing new categories. The frequency ordering of that taxonomy is your rule backlog, and it beats any list you write from first principles.

**Binary, not scored.** Where you do need a judge, make it pass or fail. Likert scales produce inconsistent labels and a middle value that hides uncertainty. Split into several binary sub-checks if you need finer detail.

**Mechanical migration is a solved problem.** When a standard changes and existing code has to move, that is a large-scale change, not an agent task. Google's numbers: 700+ independent changes a day, 15,000+ files, generated by tooling with humans owning only the process. ast-grep, JSSG and OpenRewrite are the small-scale versions. Sharding plus a cleanup phase that stops backsliding is the part people skip.

## What to watch out for

- A `Stop` hook that always blocks is an infinite loop. The standard fix is a marker file so it blocks once and then steps aside.
- `PostToolUse` cannot un-run the tool. If the action must not happen, it has to be `PreToolUse`.
- Hook output shape is easy to get wrong and fails silently. `PreToolUse` uses `hookSpecificOutput.permissionDecision`; other events use a top-level `decision`. Exit 2 blocks, and every other non-zero exit is logged and ignored. Stray stdout in an exit-0 hook becomes invalid JSON, so send diagnostics to stderr.
- Synchronous hooks sit on the hot path of every matching tool call. Keep them to single-digit seconds or run them async.
- Overusing ellipses in Semgrep patterns causes both false positives and slow scans. Scope with `paths` and `pattern-inside`.
- Suppression comments are a trapdoor. `// nosemgrep`, `eslint-disable`, `@ts-ignore` and `any` are all things a compliant agent will reach for to make a check go quiet. Decide the policy before you ship the check, and consider counting suppressions as its own fitness function.
- Coverage gates exist because agents delete tests. CodeScene calls this out explicitly, and structural quality checks will not catch it.
- Agents perform worse in unhealthy code. CodeScene's figures are up to 50% more tokens on low code-health files, with best results above a health score of 9.5. Refactoring before pointing agents at a module is a real intervention, not tidiness.

## Gaps in the literature

- **No agent-audience false positive data.** Every number in here is measured on humans clicking through code review. The most important quantity for this design, how much noise an agent tolerates before it starts working around checks, is unmeasured.
- **No treatment of gaming.** Coverage regression is the only defence anyone names. Nothing covers weakened assertions, widened types, added suppressions or checks quietly excluded from config. This is the obvious failure mode of deterministic enforcement against a compliant optimiser and the literature has not caught up.
- **No head-to-head on ast-grep versus Semgrep.** Both are here, neither source compares them, and the choice matters if you are committing to a rule language.
- **No latency or cost budget for the inner loop.** Hooks add time to every tool call. Nobody quantifies what that does to a long agent session.
- **Fitness functions predate agents.** Ford's framing is aimed at deployment pipelines. Nothing adapts it to a check that has to fire inside a single agent turn.
- **Google's model assumes a tools team.** The rollout process, the codebase-wide fix before enabling, the dedicated support function, none of it scales down cleanly to one person and one repo. A lighter governance story is needed.
- **The EvalGen paper is about judge alignment in general, not code.** Useful for criteria drift and nothing else here.

## Where this points

- Pull 20 to 30 real agent-authored diffs from this repo, open-code the failures in free text, then group them. Stop when new diffs stop producing new categories. That taxonomy is the standards list.
- Take the top category and place it on the enforcement ladder at the highest rung that can express it. Resist writing a skill for anything a type or a linter can hold.
- Ship the first check advisory. Non-blocking CI plus a `PostToolUse` hook that prints to the agent. Measure hit rate and effective false positive rate for a fortnight before promoting it to blocking.
- Write the suppression policy before the second check exists, and add a check that counts suppressions.
- Decide ast-grep or Semgrep now, on one representative rule from the taxonomy, prototyped in both. Do not defer this until you have twenty rules in the losing one.
