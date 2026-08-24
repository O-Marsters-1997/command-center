#!/bin/sh
# Fake agent: commits one file and exits 0. $1 worktree, $2 settings, $3 prompt.
# Logs its own invocation to $CC_AGENT_LOG when set, so an e2e script can assert on the number
# of times an agent actually ran, distinct from tp.log's cuts or events' run_launched rows.
set -eu
if [ -n "${CC_AGENT_LOG:-}" ]; then printf '%s %s\n' "$(basename "$0")" "$1" >> "$CC_AGENT_LOG"; fi
cd "$1"
printf 'agent was here: %s\n' "$(basename "$1")" > agent.txt
git add agent.txt
git commit -q -m "agent: commit from commits.sh"
