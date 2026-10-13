# 18. cc has no login

**Date:** 2026-10-13 · **Status:** accepted

## Context

cc was a single-owner tool behind a password, sessions and per-account backoff. The server binds
`127.0.0.1`, and Caddy is the only path from outside, reached over a tailnet. The login guarded one
owner on a network that already limits who can connect, and cost a users table, a sessions table, a
CLI to create accounts, a sweeper and a `Secure`-cookie caveat in local dev.

## Decision

cc has no login and trusts the network it is reached on. Every route serves without a cookie.
`http.CrossOriginProtection` stays around the mux, so a cross-origin browser POST is still refused.

Exposing the Caddy port beyond a trusted network is the operator's decision; nothing in cc defends
it.

## Consequences

A deployment that makes cc reachable from the public internet needs its own gate in front, such as
Caddy basic auth or a VPN. cc adds none.
