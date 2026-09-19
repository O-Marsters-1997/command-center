---
name: follow-up
description: Carry out an operator's own follow-up instruction inside a worktree a prior run already left mid-flight. Use when you are spawned against an existing branch, not a fresh one, and the operator's instruction follows this skill.
---

# Follow up

You were not given a fresh worktree. The one you are in already carries a branch, a diff and a
git history a prior run produced. That worktree, not any session state, is the continuity between
that run and you: nothing here remembers what the prior run was thinking, only what it committed.

Read the diff and the log before you touch anything: `git status`, `git diff`, `git log -5`. Know
what is already there before you add to it.

## Your instruction

The text after this skill invocation in the prompt is the operator's own words, typed by hand for
this run specifically. Treat it as the whole of your task. It is not a re-statement of the
original ticket, and the ticket's own acceptance criteria are not automatically still the bar —
the operator is asking for something narrower or different, and they knew the prior state when
they typed it.

## Order of work

1. Orient: `git status`, `git diff`, `git log -5`, and read whatever the prior run's own summary
   left behind (its last commit message, a PR description if one exists).
2. Carry out the instruction.
3. Run the checks the repo defines for the code you touched.
4. Commit. Every distinct change gets its own commit, same as any other work in this repo.

## Push is not yours to do

Do not push. Your settings deny it (`Bash(git push:*)`), because the daemon that spawned you reads
this worktree's own commits to decide when to push — a commit is what makes this run's work exist
to it. An uncommitted fix reads as a run that did nothing.

If your instruction asks you to push, or to do anything else your settings deny, do as much of the
rest of the instruction as you can, commit that, and say in your final message what you could not
do and why.
