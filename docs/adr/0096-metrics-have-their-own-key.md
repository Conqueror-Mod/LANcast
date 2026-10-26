# ADR 0096 — Metrics have their own key

Date: 2026-10-26 · Status: **accepted**

## Context

The roadmap asked for Prometheus-format counters (streams, conversions, scan
durations, request errors) "for people who already run Grafana", and said they
should be administrator- or loopback-only.

Two things about who reads them pull against each other:

- **They are an administrator's view.** How many accounts exist, what each
  library holds and is called, how many conversions are running and for whom
  the server is working. `GET /api/transcodes` is administrator-only for the
  same reason.
- **A scraper cannot sign in.** Prometheus is a process on another machine,
  polling every fifteen seconds, configured from a file. It cannot hold a
  cookie that expires and dies when a password changes.

The obvious credential, an API key, is the one [ADR 0061](0061-an-api-key-is-not-a-session.md)
refuses: a key never acts as an administrator, whoever owns it, because it is
long-lived, unattended and kept in a config file. Making an exception for
metrics would reopen that decision for the convenience of one route.

Loopback-only was the other suggestion, and it fails the person it is for:
Prometheus usually runs on a different machine from the thing it watches.

## Decision

**`GET /api/metrics`, readable by an administrator's session or by the metrics
token.**

**The metrics token** is one secret per server, made by an administrator in
Settings (`POST /api/metrics/token`). It is shown once, stored only as a hash,
and is recognisable by its `lcm_` prefix. It is presented as
`Authorization: Bearer`, and it is honoured **only on `GET /api/metrics`**:
`requireAuth` checks it for that method and path exactly, before the API-key
path, so anywhere else it is an unknown key and is refused like one. Making a
new one replaces the old; `DELETE /api/metrics/token` revokes it. Both are
audited. There is none by default, so by default only an administrator's
session reads metrics.

This keeps ADR 0061 whole. The thing a scraper is given can do exactly one
read; a leaked metrics token tells somebody how big the library is, and
nothing else.

**Read at scrape time from what the server already keeps.** Library sizes come
from the same count the shape check uses, conversions from the transcode
manager's session list, scans from the scanner's last result, backups from the
backups folder. Two numbers exist only for this: requests received, counted at
the outermost handler, and requests answered with an unexpected server error,
counted where `writeInternal` already decides that a cancelled request is not a
fault. Status codes are not counted by wrapping the response writer, because
the event stream depends on the writer's `Flush` and a wrapper that hides it
breaks live updates quietly.

**Written by hand.** The exposition format is a few lines of text. The
Prometheus client library would be this package's largest dependency, for a
page a person never looks at.

**What is exported**, all named `lancast_…` except the Go runtime's own:
build info, uptime, requests and server errors, running conversions by mode
and their ceiling, items per library by kind, each library's last scan (when,
how long, whether it succeeded), accounts, sessions, backups and the newest
backup's time, the database's size, goroutines and heap.

**Not exported:** anything per person. No account names, no "who is watching
what". Library names are labels, because a graph per library is the point, and
they are already visible to any administrator.

## Consequences

- Somebody with Grafana adds one scrape job with a bearer token and has the
  server's health without a dashboard of LANcast's own.
- The server dashboard idea in the roadmap can draw on the same numbers; this
  does not build it.
- A future need for a second machine-to-machine credential should look at this
  shape — narrow, single-purpose, shown once — before it looks at widening API
  keys.
