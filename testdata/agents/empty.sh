#!/bin/sh
# Fake agent: does nothing and exits 0, leaving HEAD where it was.
# Logs its own invocation to $CC_AGENT_LOG when set -- see commits.sh's comment.
if [ -n "${CC_AGENT_LOG:-}" ]; then printf '%s %s\n' "$(basename "$0")" "$1" >> "$CC_AGENT_LOG"; fi
exit 0
