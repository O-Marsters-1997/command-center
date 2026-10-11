#!/bin/sh
for arg in "$@"; do if [ "$arg" = claude-haiku-5-5 ]; then exit 0; fi; done
set -eu
question=$(grep -o '[^ ]*\.question' "$3" | head -n 1)
printf 'Which database?\nPostgres or SQLite.\n' > "$question"
