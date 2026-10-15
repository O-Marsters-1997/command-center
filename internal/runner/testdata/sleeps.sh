#!/bin/sh
set -eu
sleep 30 &
echo $! > "$1/child.pid"
echo $$ > "$1/pgid"
: > "$1/ready"
wait
