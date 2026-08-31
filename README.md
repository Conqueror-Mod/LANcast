# LANcast

A self-hosted media server and library service. Plex's streamlined
functionality, Kodi's customizability and versatility, and neither one's
compromises.

> **Status: v0.8.34 · M0–M4 built and released.** Point it at a folder and it
> scans, identifies, and plays — direct play, remux, or transcode, with working
> seek and resume and hardware encoding. Films, TV, **music**, and **pictures**
> are all first-class. Real metadata, artwork, ratings, subtitles, playlists,
> multi-user accounts, per-account profiles, HTTPS, a sandboxed plugin runtime,
> a native desktop window, and a server that can update itself. Corrections you
> make are locked and survive rescans.
>
> [docs/roadmap.md](docs/roadmap.md) is the honest ledger — every release, what
> shipped, and what was *not* looked at before it shipped.

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
internet for the server to work, and the server does not do network things you
did not ask for. Remote access is opt-in and self-owned (WireGuard, Tailscale,
your own reverse proxy) — never a relay you rent.

## What it does

**Libraries.** Movies, TV shows, music, and pictures, all on one media table
with no per-type schema ([ADR 0002](docs/adr/0002-one-wide-media-item-table.md),
[ADR 0024](docs/adr/0024-music-libraries.md),
[ADR 0028](docs/adr/0028-pictures-library.md)). A library can span several
roots. Scanning marks missing files, never deletes them.

**Metadata that stays corrected.** TMDB and Kodi-style `.nfo` sidecars behind
one provider contract, with confidence scoring, a review queue, and a match
score you can read. Edit a field and it locks: no refresh, rescan, or merge
touches it again ([ADR 0008](docs/adr/0008-field-level-locking.md)).

**Playback anywhere.** ffprobe decides direct play, remux, or transcode *and
states its reason* ([ADR 0012](docs/adr/0012-probe-before-transcode.md)).
Hardware encoding (NVENC, QSV, AMF, VideoToolbox) is chosen by a real test
encode rather than a capability list. Clients declare what they decode, and the
server widens the profile rather than re-encoding what the machine could have
played.

**Music and pictures, properly.** Album view with a numbered track list, an
audio mode in the player, and a docked mini-player that survives leaving the
page. Embedded tags are the authority for a track, because the file carries the
answer. Pictures added the case nobody predicted: the file *is* its own artwork.

**Playlists.** `.m3u` files on disk seed playlists and are not the playlists —
once a human edits the membership, no rescan may undo it
([ADR 0030](docs/adr/0030-playlists-and-m3u.md)).

**Live TV** *(in progress)*. M3U channel sources with an EPG, for people who
already have a stream to point at. Media Source Extensions playback is built and
**off by default**, and a browser without it falls back rather than failing
([ADR 0013](docs/adr/0013-transcode-pipeline.md),
[ADR 0036](docs/adr/0036-epg.md)). Usable, and the one feature here not being
called finished.

**People.** Multi-user accounts with admin and member roles, per-user watch
state, profile pages with real history and totals, and viewing that is private
by default and shared only by an explicit opt-in
([ADR 0035](docs/adr/0035-who-may-see-whose-viewing.md)).

**Watch Together.** Synchronised rooms on one server, and paired servers that
can see each other's presence and admit remote guests under a host-set cap
([ADR 0044](docs/adr/0044-server-identity-and-peering.md) through
[ADR 0047](docs/adr/0047-remote-streaming-is-capped-by-the-host.md)).

**Plugins.** Signed `.lcplugin` bundles running as WebAssembly under wazero,
deny-by-default capabilities, and a two-step install that grants them
explicitly ([ADR 0020](docs/adr/0020-plugin-isolation-boundary.md),
[ADR 0021](docs/adr/0021-plugin-distribution-and-trust.md)).

**Its own window.** `LANcast-Client` opens a WebView2 window rather than handing
a URL to a browser — which is what lets it pin the server's certificate instead
of showing you a warning it cannot resolve
([ADR 0023](docs/adr/0023-native-desktop-client.md)).

## Requirements

- **Go 1.25+** — required to build; nothing at all to *run* a release
- **ffmpeg** — needed for probing and conversion. You do not have to install it
  yourself: setup offers to fetch a pinned, checksum-verified build as a ticked
  option on the form you already submit, and Settings has the same button
  ([ADR 0043](docs/adr/0043-media-tools-are-fetched-not-bundled.md),
  [ADR 0048](docs/adr/0048-media-tools-install-themselves-on-first-run.md)).
  Untick it and bring your own on `PATH`, or go without — LANcast still runs and
  serves direct-play files, and says so plainly rather than failing.

SQLite is *not* a requirement — LANcast uses a pure-Go driver, so there is no
cgo and no C toolchain needed on any platform. See
[ADR 0001](docs/adr/0001-go-and-pure-go-sqlite.md).

## Build and run

```bash
go test ./... && go run ./cmd/lancastd -addr :8080
```

Then open <http://localhost:8080>, create the first account, add a library
pointing at a folder of media, and scan it.

| Flag | Default | Meaning |
|---|---|---|
| `-addr` | `:8080` | Listen address |
| `-data` | `%APPDATA%/LANcast` (platform config dir) | Where `lancast.db` lives |
| `-v` | off | Verbose logging |

Build a distributable server binary:

```bash
go build -o LANcast-Server ./cmd/lancastd
```

The client UI is a React + TypeScript app in `web/`, built by Vite and committed
into `internal/web/dist` so a built server is genuinely one file. After changing
it, run `npm --prefix web test` and `npm --prefix web run build`, and commit the
rebuilt `dist`.

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

## Install a release

Releases ship two executables — `LANcast-Server` (the daemon) and
`LANcast-Client` (the launcher you open) — with no runtime to install. On
Windows the installer registers the server as a service and adds a shortcut; on
Linux/NAS it runs as a systemd service. Full steps, including the one
data-directory rule that matters most, are in
**[docs/install.md](docs/install.md)**.

The server can check for, download, verify, and stage an update that swaps
itself in on the way down. It swaps `LANcast-Server` only — a change to the
client, the window, or the installer needs the installer.

## Documentation

| Document | What it covers |
|---|---|
| [docs/install.md](docs/install.md) | Installing a release, the service, and the data-dir rule |
| [docs/architecture.md](docs/architecture.md) | Package map, request, streaming and scan lifecycles |
| [docs/api.md](docs/api.md) | HTTP contract — the promise clients depend on |
| [docs/design.md](docs/design.md) | Visual design system, the gold rule, keyboard model |
| [docs/metadata.md](docs/metadata.md) | Providers, matching, locking, artwork |
| [docs/security.md](docs/security.md) | What is protected, what is not — **read before exposing it** |
| [docs/roadmap.md](docs/roadmap.md) | Every release, every area's status, and what each pass taught |
| [docs/adr/](docs/adr/) | 50 decision records — why things are the way they are |

The `*-plan.md` files in `docs/` are the plans particular areas were built
from. They are kept as written rather than revised, so they record what was
expected at the time; the roadmap and the ADRs record what actually happened.

## Security

LANcast is guarded by accounts — the first, created on first run, is an admin;
others can be members who browse and play but cannot manage libraries, settings,
or users.

**Until the first account exists, it listens on `127.0.0.1` only** — reachable
from the machine it runs on, and nowhere else. The API exposes filesystem
browsing and library creation at arbitrary paths, so an open port with no
password is arbitrary read access. Create the account, restart, and it binds the
network so other devices can reach it.

**Once it binds the network, it serves HTTPS.** Supply your own certificate
(`tls_cert_file` / `tls_key_file`) or let LANcast generate a self-signed one —
either way the password and session cookie are encrypted on the LAN. A
self-signed cert shows a browser warning until you trust it; the LANcast window
pins the key instead and does not.

**Do not port-forward it anyway.** A self-signed cert cannot prove the server's
identity to the public internet. For access away from home, use a VPN that puts
your device on the LAN (Tailscale, WireGuard), or a reverse proxy terminating
TLS with a real certificate. See [docs/security.md](docs/security.md).

## License

**LANcast is © 2026 Conqueror-Mod and licensed under the
[GNU Affero General Public License, version 3](LICENSE) (AGPL-3.0-or-later).**

You may run it, study it, change it and share it. If you distribute a modified
version — **or run one as a service other people use over a network** — you must
offer those people the source of your version under the same terms. That last
clause is why this is the Affero GPL rather than the ordinary GPL: a media
server can be run for other people without ever being *distributed*, and plain
GPL would not reach it.

**A commercial licence is available** for anyone who wants to build on LANcast
without those obligations — see [COMMERCIAL.md](COMMERCIAL.md).

*Releases up to and including v0.8.44 were published under the MIT licence and
remain available under it. The change applies to work after that; it does not
retract anything already released.*

Third-party components keep their own licences — see [NOTICE](NOTICE).

Vendored third-party code keeps its own license and notices; see
[internal/webview2/LICENSE](internal/webview2/LICENSE),
[internal/webview2/PROVENANCE.md](internal/webview2/PROVENANCE.md), and
[web/vendor/hls.js/LICENSE](web/vendor/hls.js/LICENSE).
