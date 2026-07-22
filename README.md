# LANcast

A self-hosted media server and library service. Plex's streamlined
functionality, Kodi's customizability and versatility, and neither one's
compromises.

> **Status: M1 works.** Scan a folder, browse it in a browser, click a title,
> and it plays with working seek and resume. No metadata or artwork yet — that
> is M2. See [docs/roadmap.md](docs/roadmap.md) for what exists and what does not.

## Why

Kodi had the right bones — everything was extensible — but never the sustained
support to stay usable. Plex arrived cleaner and better made, then began moving
capabilities that used to be free behind a paywall and a mandatory cloud
account. Both directions cost you the same thing: control over your own library.

LANcast is an attempt to get that back, without pretending the two approaches
are opposites. Plex's polish came from one opinionated pipeline. Kodi's power
came from everything being a plugin. You can have both, if the core stays small
and honest and the extension surface is a real contract rather than an
afterthought.

## Principles

These are load-bearing. Every design decision in `docs/` traces back to one.

**Server owns truth.** Scanning, metadata, transcoding, users, and playback
state all live server-side. Headless, API-first.

**Clients are thin.** One documented HTTP API, so a third-party TV or mobile
client is a first-class possibility rather than a fork.

**Everything interesting is a provider.** Metadata scrapers, artwork,
subtitles, scanners — and eventually what counts as a library type — sit behind
stable interfaces. Kodi's real win was that TV shows weren't hardcoded.

**No phone-home.** Local-first, LAN-first. Nothing is required to reach the
internet for the server to work. Remote access is opt-in and self-owned
(WireGuard, Tailscale, your own reverse proxy) — never a relay you rent.

## Requirements

- **Go 1.22+** — required to build
- **ffmpeg** — not needed yet; required from M3 (transcoding) onward

```bash
winget install --id GoLang.Go -e && winget install --id Gyan.FFmpeg -e
```

Open a new shell afterward so `PATH` picks both up.

SQLite is *not* a requirement — LANcast uses a pure-Go driver, so there is no
cgo and no C toolchain needed on any platform. See
[ADR 0001](docs/adr/0001-go-and-pure-go-sqlite.md).

## Build and run

```bash
go test ./... && go run ./cmd/lancastd -addr :8080
```

Then open <http://localhost:8080>, add a library pointing at a folder of media,
and scan it.

| Flag | Default | Meaning |
|---|---|---|
| `-addr` | `:8080` | Listen address |
| `-data` | `%APPDATA%/LANcast` (platform config dir) | Where `lancast.db` lives |

Build a distributable binary:

```bash
go build -o lancastd ./cmd/lancastd
```

## Documentation

| Document | What it covers |
|---|---|
| [docs/architecture.md](docs/architecture.md) | Package map, request and scan lifecycles |
| [docs/api.md](docs/api.md) | HTTP contract — the promise clients depend on |
| [docs/design.md](docs/design.md) | Visual design system, the gold rule, keyboard model |
| [docs/metadata.md](docs/metadata.md) | Providers, matching, locking, artwork (M2) |
| [docs/roadmap.md](docs/roadmap.md) | All 26 planning areas and their status |
| [docs/adr/](docs/adr/) | Why decisions were made the way they were |

## License

Not yet chosen. LANcast is a personal project built to be open-sourced later;
the license gets decided before the repo goes public.
