# ADR 0089 — The port moves, and comes back if it cannot

Date: 2026-10-17 · Status: **accepted** 2026-10-17

The plan is [settings-plan.md](../settings-plan.md), Network: "Listen port —
today a command-line flag baked into the service at install. Changing it means
rewriting the service's command line and restarting. The loopback rule must
survive unchanged." Chris decided on 2026-10-17:
- **Restart now, and roll back if it fails.**
- **Warn about friends**, rather than extend federation to announce a port.
- **Ports 1024–65535, except 443 and 8200.**

## Context

The installed service starts as
`"…\LANcast.exe" service run -data C:\ProgramData\LANcast -addr :8080`. That
command line is the only place the port is recorded. Several things assumed
it would never change:

- **The desktop window** opens `:8080` unless started with `-addr`.
- **`restore` and `reset-auth`** check `:8080` to make sure the server is
  stopped before they touch its database. If the server were answering on
  another port, that check would report it stopped while the database is
  still open.
- **Friends' servers** record this server's address with its port. They only
  update the IP from where a connection came from, keeping the port they
  already had (ADR 0044 §5, `learnedAddress`).
- **Firewall rules** that allow the port by number. On Aither the rule is
  named "web" and allows inbound TCP 8080.

## Decision

**1. The command line stays the one record.** Settings rewrites `-addr` in
the service's configuration with `ChangeServiceConfig`, changing nothing else
about the service. A copy in `config.json` would be a second answer to "which
port", and the two would disagree the first time somebody edited one of them.
The service runs as LocalSystem, which is allowed to change its own
configuration, so no elevation prompt is needed. The installer keeps an
existing service as it is on upgrade, so an update does not undo the change.

**2. Only the port changes.** The host part is kept. A restarted server binds
through `bindAddr` exactly as it did before, so with no password set it still
answers only on 127.0.0.1. The loopback rule is not touched.

**3. The port is checked before anything changes.** `PUT /api/network/port`
refuses the following:

| Port | Why it's refused | Answer |
|---|---|---|
| Below 1024 or above 65535 | out of range | `400` |
| 443 | the public certificate's second listener uses it | `400` |
| 8200 | DLNA uses it | `400` |
| The current port | nothing would change | `400` |
| One something else holds | in use | `409` |

The in-use check opens and closes a socket on the address the server would
bind. That never opens anything wider than the loopback rule allows.

**4. It comes back if it can't start.** The restart goes through the same
detached helper an update uses (`service restart`), with `-verify` set to the
new address and `-fallback` to the old one.
- If the new port answers within 30 seconds, the change stands.
- If it doesn't, the helper puts the old `-addr` back and starts the service
  again, whatever state Windows' own recovery restarts have left it in.

Either way the helper writes the outcome to `port-change.json` in the data
folder. The process that was asked has gone by then, so this file is how
`GET /api/network` can report it (as `last_port_change`, for a day).

**5. Only the installed Windows service can change its port.** Anywhere else,
the port is the `-addr` the server was started with, and the Network page
says so instead of offering a change that couldn't happen. A systemd unit
belongs to whoever wrote it.

**6. Everything on this machine follows the service's command line.**
`service.ConfiguredAddr` reads it without elevation. The desktop window
opens it unless started with `-addr`. `restore` and `reset-auth` check it.
An open window keeps watching it, so it follows the move instead of waiting
on an address that will never answer again.

**7. The page follows the server.** After the `202`, the Network page:
- waits for the new port to answer, then goes there;
- if the old port answers again instead, says the server went back;
- if neither has answered after a minute, goes to the new port anyway.

A browser that hasn't trusted the certificate there yet can't see the port
answer, and the page it lands on is where it gets asked.

## Consequences

- **Firewall:** LANcast makes no firewall rule for the new port, which is a
  standing rule. The confirmation says that other devices need one.
- **Friends:** paired servers and existing invites keep the old port, and
  friends need a new invite. Friends who reach this server by its public
  name on 443 are unaffected.
- **Protocol:** announcing a new port over federation was considered and
  declined for now. It would be a small extension, sent over the mutually
  authenticated connection that already teaches a peer a moved IP.
- **What it costs:** the change interrupts anyone watching, the same as an
  update restart.
