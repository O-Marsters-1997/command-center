#!/bin/sh
set -eu
cd "$1"
printf 'agent was here: %s\n' "$(basename "$1")" > agent.txt
git add agent.txt
git commit -q -m "agent: commit from commits.sh"
