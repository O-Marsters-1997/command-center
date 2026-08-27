---
name: resolve-merge-conflict
description: Resolve a conflicted merge or rebase in a git worktree, deterministically, when two parallel branches touched the same code. Use when a worktree is left mid-merge and you have to reconcile both sides.
---

# Resolve a merge conflict

Two branches were written in parallel against the same base. One landed, the other now has to
absorb it. Your job is to make the branch carry both intents and nothing else.

You are not reviewing either change. You are not finishing the ticket. You are reconciling.

## Order of work

1. `git status` and `git diff --name-only --diff-filter=U`. That list is the whole job.
2. For each conflicted path, decide which class it is (below) before editing it.
3. Resolve. Stage each file as you finish it.
4. Run the checks the repo defines, not a subset.
5. Commit the merge with the default message. Push nothing.

## Classify every conflicted path first

**Generated or golden files.** Never hand-merge them. Take either side, then regenerate and let
the tool overwrite. Reconciling two machine-written files by hand produces a third thing that
matches neither. If you cannot find the generator, stop: this is not resolvable by hand.

**A file one side deleted, renamed or moved.** Git reports this as modify/delete, and it is the
one class where the diff lies to you. The other side's edit still has to land, at the new path.
Read the deleting side's commit to find where the content went, apply the surviving edit there,
and delete the old path. Do not resurrect a file the other side deliberately removed.

**A list, table or enum both sides appended to.** Keep both entries. Then find everything that
counts or switches over that list (an exhaustiveness test, a count assertion, a switch with no
default) and update it for the new total. This is the class that compiles and then fails one
test with an off-by-one.

**Both sides edited the same logic.** The only class that needs real judgement. Read both
commits, not just the conflict markers, so you know what each side was trying to do. If both
intents can hold, write the version that satisfies both. If they genuinely contradict, stop and
leave it unresolved: a wrong guess here is worse than a conflict.

**Imports, struct fields, adjacent unrelated lines.** Take both sides. Order them the way the
file already orders things.

## Before you commit

- No conflict markers left anywhere: `git diff --check` and grep for `<<<<<<<`.
- The build passes and the whole test suite passes, not the tests you touched. A conflict
  resolution that breaks a test three packages over is the normal outcome, not a surprise.
- Regenerated files are staged along with the source change that moved them.
- `git diff --staged` against each parent contains nothing that is not the merge. If you find
  yourself improving code, you have left the job.

## When to stop instead

Commit nothing and stop if: the two sides contradict, a generated file has no generator you
can find, the test failures are not obviously downstream of the conflict, or resolving would
mean rewriting a side rather than reconciling it.

Stopping is a correct outcome. It hands the merge back exactly as you found it, which is where
it would have been anyway. A confidently wrong resolution is the only outcome that costs
anybody anything.
