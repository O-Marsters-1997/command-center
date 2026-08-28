#!/bin/sh
# Fake agent: like commits.sh, but sleeps briefly before committing, so a script can catch it
# still running across an instance boundary (crash-recovery style) before it finishes.
# $1 worktree, $2 settings, $3 prompt.
set -eu
if [ -n "${CC_AGENT_LOG:-}" ]; then printf '%s %s\n' "$(basename "$0")" "$1" >> "$CC_AGENT_LOG"; fi
sleep 1
cd "$1"
printf 'agent was here: %s\n' "$(basename "$1")" > agent.txt
git add agent.txt
git commit -q -m "agent: commit from commits_slow.sh"
