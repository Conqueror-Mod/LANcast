# LANcast

A self-hosted media server and library service. Plex's streamlined
functionality, Kodi's customizability and versatility, and neither one's
compromises.

> **Status: M2 works.** Scan a folder, browse it with posters and metadata,
> click a title, and it plays with working seek and resume. Corrections you
> make are locked and survive rescans. Transcoding is M3.
> See [docs/roadmap.md](docs/roadmap.md) for what exists and what does not.

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
| `-v` | off | Verbose logging |

### Metadata

Optional. Add a free [TMDB](https://www.themoviedb.org/settings/api) key under
Settings for posters, synopses, cast, and artwork.

**LANcast works without one** — it falls back to filenames and Kodi-style
`.nfo` sidecars, and never reports the missing key as an error. That is the
no-phone-home principle being real rather than decorative. The key is stored
`0600` outside the database and is never readable back through the API.

Use the **API Key (v3 auth)** — the 32-character hex value. The API Read Access
Token beside it is a v4 bearer JWT and will be rejected.

> This product uses the TMDB API but is not endorsed or certified by
> [TMDB](https://www.themoviedb.org). Attribution is a condition of their free
> tier and is also shown in the app's settings screen.

### Ratings

Optional. Add a free [OMDb](https://www.omdbapi.com/apikey.aspx) key under
Settings to show **Rotten Tomatoes, Metacritic, and IMDb** scores on the detail
page, alongside the TMDB rating.

The key is **your own OMDb account**, not something LANcast supplies: nothing is
fetched until you enter one, the key is stored `0600` outside the database and is
never readable back through the API, and clearing it turns the feature off again.
LANcast is a client pointed at your OMDb subscription — it does not redistribute
ratings data. Use of the key is subject to
[OMDb's terms](https://www.omdbapi.com/legal.htm); the free tier is intended for
personal, non-commercial use, and each score is shown labelled with its source.

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
| [docs/security.md](docs/security.md) | What is protected, what is not — **read before exposing it** |
| [docs/roadmap.md](docs/roadmap.md) | All 26 planning areas and their status |
| [docs/adr/](docs/adr/) | Why decisions were made the way they were |

## Security

LANcast is guarded by accounts — the first, created on first run, is an admin;
others can be members who browse and play but cannot manage libraries, settings,
or users.

**Until the first account exists, it listens on `127.0.0.1` only** — reachable
from the machine it runs on, and nowhere else. Create the account in the
browser, restart, and it binds the network so other devices can reach it.

**Once it binds the network, it serves HTTPS.** Supply your own certificate
(`tls_cert_file` / `tls_key_file`) or let LANcast generate a self-signed one —
either way the password and session cookie are encrypted on the LAN. A
self-signed cert shows a browser warning until you trust it.

**Do not port-forward it anyway.** A self-signed cert cannot prove the server's
identity to the public internet. For access away from home, use a VPN that puts
your device on the LAN (Tailscale, WireGuard), or a reverse proxy terminating
TLS with a real certificate. See [docs/security.md](docs/security.md).

## License

Proprietary — all rights reserved. See [LICENSE](LICENSE).

LANcast is still a personal project and the repository is private. Open-sourcing
remains the intent; the permissive license gets chosen before the repo goes
public, and this one is replaced at that point.
