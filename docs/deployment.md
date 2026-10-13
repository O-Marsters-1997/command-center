# Deployment

## What Caddy does

The checked-in `Caddyfile` (repo root) terminates TLS on `$CC_DOMAIN` and reverse-proxies
everything to `127.0.0.1:7777`, the port cc already binds by default (`internal/config/config.go`,
`defaultPort`). Caddy's automatic HTTPS is the whole TLS story: no certificate handling, no
bind-address config, and no proxy-header code live in cc itself. `App.Run` keeps binding
`127.0.0.1` exactly as it does today (`internal/app/app.go`) — Caddy is the only thing that ever
listens on a public interface.

## cc never trusts the proxy

Nothing in cc reads `X-Forwarded-For` or `RemoteAddr`, and cc has no login: it trusts the network it
is reached on ([ADR 18](adr/0018-cc-has-no-login.md)). The Caddyfile carries no `X-Forwarded-*`
header configuration; nothing on the other end would look at it.

## Out of scope

Caddy secures the transport between a browser and cc. It does not touch:

- **Postgres.** Still `cc:cc` with `sslmode=disable` (`internal/config/statedir.go`,
  `defaultDatabaseURL`). Fine on loopback/localhost-only Postgres; not fine if Postgres itself is
  ever exposed off the box.
- **`~/.config/command-centre/`.** Holds GitHub credentials and repo checkouts on disk, unencrypted,
  same as today.
- **The SSH surface.** Whatever SSH exposure the host already has is untouched by this work.

Putting a Caddyfile in front of cc is not a claim that the box is hardened. Whoever can reach the
Caddy port can drive cc, so expose it only on a network you trust, such as a tailnet.
