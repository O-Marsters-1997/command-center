#!/bin/sh
set -eu
if [ -n "${CC_AGENT_LOG:-}" ]; then printf '%s %s\n' "$(basename "$0")" "$1" >> "$CC_AGENT_LOG"; fi
for attempt in $(seq 1 3000); do
	if [ -f "$HOME/agent-release" ]; then break; fi
	sleep 0.01
done
if [ ! -f "$HOME/agent-release" ]; then exit 1; fi
cd "$1"
printf 'agent was here: %s\n' "$(basename "$1")" > agent.txt
git add agent.txt
git commit -q -m "agent: commit from commits_slow.sh"
if [ -n "${CC_AGENT_LOG:-}" ]; then printf 'commits_slow.sh %s\n' "$(basename "$1")" >> "${CC_AGENT_LOG%.log}-done.log"; fi
