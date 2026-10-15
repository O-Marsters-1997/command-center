#!/bin/sh
set -eu
prompt=$3
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
if [ "$mode" = resume ]; then
	cp "$prompt" "$(dirname "$prompt")/answer.txt"
	if [ -n "${CC_AGENT_LOG:-}" ]; then printf 'asks.sh resume\n' >> "${CC_AGENT_LOG%.log}-done.log"; fi
	exit 0
fi
question=$(grep -o '[^ ]*\.question' "$prompt" | head -n 1)
printf 'Which database?\nPostgres or SQLite.\n' > "$question"
if [ -n "${CC_AGENT_LOG:-}" ]; then printf 'asks.sh session-id\n' >> "${CC_AGENT_LOG%.log}-done.log"; fi
