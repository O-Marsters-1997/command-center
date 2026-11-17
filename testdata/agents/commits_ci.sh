#!/bin/sh
set -eu
cd "$1"
printf 'registry=x\n' > .npmrc
git add .npmrc
git commit -q -m "agent: touch denied config"
if [ -n "${CC_AGENT_LOG:-}" ]; then printf 'commits_ci.sh %s\n' "$(basename "$1")" >> "${CC_AGENT_LOG%.log}-done.log"; fi
