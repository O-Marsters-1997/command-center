#!/bin/sh
# Fake agent: commits one file and exits 0. $1 worktree, $2 settings, $3 prompt.
set -eu
cd "$1"
printf 'agent was here\n' > agent.txt
git add agent.txt
git commit -q -m "agent: commit from commits.sh"
