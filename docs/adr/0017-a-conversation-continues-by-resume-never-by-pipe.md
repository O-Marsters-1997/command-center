# 17. A conversation continues by resume, never by pipe

**Date:** 2026-10-11 · **Status:** accepted

## Context

A follow-up, a steer and a reply to an agent's question each need the agent's own context back.

Checked against `claude` 2.1.296 on 2026-10-11:

- `--session-id <uuid>` names a new session up front, so cc assigns the id and never captures it
  from stream-json.
- `-p --resume <id>` continues the session with its context, under the same id. Its stdout carries
  only the new turns.
- SIGTERM mid-tool, which `runner.Cancel` sends, leaves a resumable session. The resumed agent sees
  that its last command was cut off.
- `--input-format stream-json` does inject a message mid-turn, but the process lives until stdin
  closes. That needs a held pipe per run, which a cc restart would close.

## Decision

Steering, reply and follow-up are one mechanism: resume the ticket's session with the owner's
message. A run that is alive is cancelled first. Nothing is piped to a running process.

- A session is the set of runs sharing `runs.session_id`. A launch or re-run starts one with
  `gen_random_uuid()` at skeleton insert; a resume run copies the id it continues. Only `agent` and
  `follow_up` runs carry a session.
- A session is resumable when its latest agent or follow-up run has a `session_id` and
  `*/<id>.jsonl` exists under `claude_projects_dir`. Otherwise a message falls back to a fresh
  follow-up, which covers a pruned transcript and a custom `agent_command` without `{session}`.
- `{session}` in `agent_command` expands to `--session-id <id>`, `--resume <id>` or nothing.
  `runner` owns the expansion and knows nothing about resumability; `loop` chooses.

## Consequences

The runner keeps its invariants: stdout is a file, no goroutine per run, and agents survive a cc
restart. A custom `agent_command` that omits `{session}` never resumes.

The system prompt says each run ends with its turn and the owner may continue the session later. The
ban on background subagents stays, because each process has no later turn to receive their result.
