# Deployment

**Source:** the auth plan's phase 7 (deployment) and integration points section, as scoped in
[issue #294](https://github.com/O-Marsters-1997/command-center/issues/294). The plan itself
(`plans/auth.md`) hasn't been committed to this repo yet.

## What Caddy does

The checked-in `Caddyfile` (repo root) terminates TLS on `$CC_DOMAIN` and reverse-proxies
everything to `127.0.0.1:7777`, the port cc already binds by default (`internal/cc/config.go`,
`defaultPort`). Caddy's automatic HTTPS is the whole TLS story: no certificate handling, no
bind-address config, and no proxy-header code live in cc itself. `App.Run` keeps binding
`127.0.0.1` exactly as it does today (`internal/cc/app.go`) — Caddy is the only thing that ever
listens on a public interface.

## cc never trusts the proxy

Nothing in cc reads `X-Forwarded-For` or `RemoteAddr`. That's deliberate, not an oversight: the
plan's brute-force defence is **per-account** backoff (keyed on the username being authenticated
against), not IP-based rate limiting. A per-account counter doesn't care what address a request
arrived from, so there is no client IP to trust, forward, or spoof around — reading a proxy header
would add a code path with nothing to protect. That also means the Caddyfile carries no
`X-Forwarded-*` header configuration; there's nothing on the other end that would look at it.

## Out of scope

This slice hardens one thing: the transport between a browser and cc. It does not touch:

- **Postgres.** Still `cc:cc` with `sslmode=disable` (`internal/cc/statedir.go`,
  `defaultDatabaseURL`). Fine on loopback/localhost-only Postgres; not fine if Postgres itself is
  ever exposed off the box.
- **`~/.config/command-centre/`.** Holds GitHub credentials and repo checkouts on disk, unencrypted,
  same as today.
- **The SSH surface.** Whatever SSH exposure the host already has is untouched by this work.

On a box reachable from the public internet, these are bigger holes than the missing password
this plan closes. Putting a Caddyfile in front of cc is not a claim that the box is hardened.

## Local dev: the Safari caveat

Chrome and Firefox accept `Secure`-flagged cookies over `http://localhost`; Safari historically
does not. Local dev without Caddy in front (i.e. talking to cc directly on `127.0.0.1:7777` over
plain HTTP) therefore wants Chrome or Firefox — Safari will silently drop the session cookie.

There's no config key to turn off the `Secure` flag for a loopback bind. It's tempting — it would
fix the Safari case — but it's a key that can be wrong in production: nothing stops it from being
set (or left set) once cc is reachable through Caddy over real TLS, silently downgrading cookie
security on a box that's no longer loopback-only. Revisit only if the Safari restriction turns out
to be genuinely annoying in practice, not preemptively.
