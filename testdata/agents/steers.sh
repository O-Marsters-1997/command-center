#!/bin/sh
set -eu
mode=none
id=
while [ $# -gt 0 ]; do
	case "$1" in
	--session-id) mode=session-id; id=$2; shift ;;
	--resume) mode=resume; id=$2; shift ;;
	esac
	shift
done
if [ -n "${CC_AGENT_LOG:-}" ]; then printf '%s %s %s\n' "$(basename "$0")" "$mode" "$id" >> "$CC_AGENT_LOG"; fi
if [ -n "$id" ]; then
	mkdir -p "$HOME/.claude/projects/fake-project"
	: > "$HOME/.claude/projects/fake-project/$id.jsonl"
fi
if [ "$mode" = session-id ]; then
	sleep 30 &
	wait
fi
if [ -n "${CC_AGENT_LOG:-}" ]; then printf 'steers.sh %s\n' "$mode" >> "${CC_AGENT_LOG%.log}-done.log"; fi
