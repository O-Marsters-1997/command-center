# 19. One injected logger, and a writer for the user

**Date:** 2026-10-13 · **Status:** accepted

## Context

cc logged through the stdlib `log` package from `main`, `app`, `loop` and `demo`, and lint exempted
those packages from the `log.*` ban. A global logger cannot be silenced or inspected in a test, and
`cc open` wrote its user-facing messages straight to `os.Stderr`. cc runs locally for one operator
(ADR 18), who reads the daemon's stderr in a terminal and collects nothing.

## Decision

- `main` builds the one `*slog.Logger`: a text handler on stderr at `Info`. There is no level flag or
  config key until something logs at `Debug`.
- Only components where an error stops travelling take the logger, as a constructor field: the
  server, the loop, the app and the demo. Code below them returns errors.
- Tests pass `slog.New(slog.DiscardHandler)`.
- Output the user asked for, such as `cc open`'s messages and URL, goes to `io.Writer`s passed into
  the subcommand, never through the logger.
- Lint bans `log.*` everywhere but `cmd/cc/main.go`.

## Consequences

A test can assert on what `cc open` tells the user. Switching to a JSON handler, for a log collector,
is a change in `main` alone.
