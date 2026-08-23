#!/bin/sh
# Fake agent: backgrounds a long sleep and waits for a signal. $1 worktree, $2 settings, $3 prompt.
# The child inherits this script's process group, so killing -pgid must reap both.
# Logs its own invocation to $CC_AGENT_LOG when set -- see commits.sh's comment.
set -eu
if [ -n "${CC_AGENT_LOG:-}" ]; then printf '%s %s\n' "$(basename "$0")" "$1" >> "$CC_AGENT_LOG"; fi
sleep 300 &
echo $! > "$1/child.pid"
# Spawn makes this script its own group leader, so $$ is the pgid the app records too. Written
# by the agent itself, it is what crash_recovery.txtar checks the page's pgid against, rather
# than taking the app's own word for it.
echo $$ > "$1/pgid"
# Written last: its presence means the pid file is already complete.
: > "$1/ready"
wait
