#!/bin/sh
# Fake agent: backgrounds a long sleep and waits for a signal. $1 worktree, $2 settings, $3 prompt.
# The child inherits this script's process group, so killing -pgid must reap both.
set -eu
sleep 300 &
echo $! > "$1/child.pid"
# Written last: its presence means the pid file is already complete.
: > "$1/ready"
wait
