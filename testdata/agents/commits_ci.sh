#!/bin/sh
for arg in "$@"; do if [ "$arg" = claude-haiku-5-5 ]; then exit 0; fi; done
# Fake agent: commits a change under .github/workflows and exits 0, so the push policy refuses
# it. $1 worktree, $2 settings, $3 prompt.
set -eu
cd "$1"
mkdir -p .github/workflows
printf 'name: x\n' > .github/workflows/x.yml
git add .github/workflows/x.yml
git commit -q -m "agent: touch CI config"
