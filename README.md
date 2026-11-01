# LANcast

A self-hosted media server and library service. Plex's streamlined
functionality, Kodi's customizability and versatility, and neither one's
compromises.

> **Status: v0.10.0 · M0–M4 built and released.** Point it at a folder and it
> scans, identifies, and plays — direct play, remux, or transcode, with working
> seek and resume and hardware encoding. Films, TV, **music**, **pictures** and
> **comics** are all first-class. Real metadata, artwork, ratings, subtitles,
> playlists, multi-user accounts with per-account library and rating limits,
> invite links, HTTPS, passkeys, two-factor sign-in, a sandboxed plugin
> runtime, a native desktop window, servers that pair with each other, and a
> server that can update itself. Corrections you make are locked and survive
> rescans.
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
your own reverse proxy, or a public name whose certificate LANcast requests only
once you turn it on) — never a relay you rent.

## What it does

**Libraries.** Movies, TV shows, music, pictures, comics and games, all on one
media table with no per-type schema
([ADR 0002](docs/adr/0002-one-wide-media-item-table.md),
[ADR 0024](docs/adr/0024-music-libraries.md),
[ADR 0028](docs/adr/0028-pictures-library.md)). A library can span several
roots. Scanning marks missing files, never deletes them, and an unchanged
library rescans in seconds.

**Metadata that stays corrected.** TMDB and Kodi-style `.nfo` sidecars behind
one provider contract, with confidence scoring, a review queue, and a match
score you can read. Edit a field and it locks: no refresh, rescan, or merge
touches it again ([ADR 0008](docs/adr/0008-field-level-locking.md)).
Collections fill themselves from the provider and show the films you don't
have yet ([ADR 0093](docs/adr/0093-a-collection-knows-its-missing-films.md)).

**Playback anywhere.** ffprobe decides direct play, remux, or transcode *and
states its reason* ([ADR 0012](docs/adr/0012-probe-before-transcode.md)).
Hardware encoding (NVENC, QSV, AMF, VideoToolbox) is chosen by a real test
encode rather than a capability list. Clients declare what they decode, and the
server widens the profile rather than re-encoding what the machine could have
played. The desktop window plays through libmpv, so most files never need
converting at all ([ADR 0067](docs/adr/0067-the-desktop-client-plays-through-libmpv.md)).

**Watching, the details.** Intros and credits are found by listening and
looking, then skipped for you
([ADR 0055](docs/adr/0055-an-intro-is-what-every-episode-shares.md),
[ADR 0083](docs/adr/0083-an-intro-skips-itself.md)); an Up next card and a
suggestion after the film. Chapters and a picture over the scrub bar
([ADR 0105](docs/adr/0105-a-preview-is-a-keyframe-every-ten-seconds.md)),
extras found beside the film
([ADR 0098](docs/adr/0098-extras-are-found-not-imported.md)), an optional
pre-roll clip ([ADR 0099](docs/adr/0099-a-preroll-plays-before-a-film-from-the-start.md)),
a watchlist, optimized copies for weak devices, and DLNA for the television
that only speaks that ([ADR 0086](docs/adr/0086-a-tv-browses-as-somebody.md)).

**Music, properly.** Album view with a numbered track list, gapless playback
and crossfades between albums, a docked mini-player that survives leaving the
page, synced lyrics, night mode and an equaliser, visualizers including
MilkDrop presets ([ADR 0091](docs/adr/0091-milkdrop-presets.md)), and loudness
evened out from the ReplayGain your tagger wrote
([ADR 0108](docs/adr/0108-music-is-levelled-by-its-own-replaygain.md)).
Embedded tags are the authority for a track, because the file carries the
answer.

**Pictures.** A timeline by capture date, people found by face
([ADR 0052](docs/adr/0052-face-grouping-runs-in-a-native-sidecar.md)), search
by what is in a photo
([ADR 0060](docs/adr/0060-semantic-photo-search-is-a-second-model-in-the-sidecar.md)),
places from GPS, off by default
([ADR 0078](docs/adr/0078-a-place-is-the-town-a-photo-was-taken-in.md)),
duplicates and near copies, and camera RAW files shown from the camera's own
picture ([ADR 0107](docs/adr/0107-a-raw-photo-shows-the-cameras-own-jpeg.md)).

**Comics.** `.cbz`, `.cb7` and `.pdf`, read a page at a time, with your place
kept ([ADR 0101](docs/adr/0101-a-comic-is-read-out-of-its-archive.md),
[ADR 0102](docs/adr/0102-a-pdf-comic-is-its-pictures.md)).

**Games.** Retro games identified by hash and played in the desktop window or a
browser, with saves and save states kept by the server
([ADR 0073](docs/adr/0073-a-retro-game-is-a-file-the-server-owns.md),
[ADR 0077](docs/adr/0077-a-browser-can-play-a-game.md)), achievements from your
own RetroAchievements account
([ADR 0090](docs/adr/0090-retroachievements-are-yours-to-connect.md)), and the
PC games installed on the machine, launched from the same hub
([ADR 0066](docs/adr/0066-a-game-belongs-to-the-machine-it-is-installed-on.md)).

**Playlists.** `.m3u` files on disk seed playlists and are not the playlists —
once a human edits the membership, no rescan may undo it
([ADR 0030](docs/adr/0030-playlists-and-m3u.md)).

**Live TV** *(parked)*. M3U channel sources with an EPG, for people who
already have a stream to point at. Media Source Extensions playback is built and
**off by default**, and a browser without it falls back rather than failing
([ADR 0013](docs/adr/0013-transcode-pipeline.md),
[ADR 0036](docs/adr/0036-epg.md)). Usable, and the one feature here not being
called finished; further work on it is parked.

**People.** Multi-user accounts with admin and member roles, per-user watch
state, profile pages with real history and totals, and viewing that is private
by default and shared only by an explicit opt-in
([ADR 0035](docs/adr/0035-who-may-see-whose-viewing.md)). An administrator can
keep a library from a member and set the content rating a member may not exceed
([ADR 0084](docs/adr/0084-a-library-can-be-kept-from-an-account.md)), and
everybody can see and sign out the devices signed in as them. A friend joins
through a link that works once
([ADR 0104](docs/adr/0104-a-friend-is-invited-by-a-link-that-works-once.md)),
and anyone can add passkeys or a two-factor code
([ADR 0100](docs/adr/0100-a-code-after-the-password-for-whoever-wants-one.md)).

**Watch Together.** Synchronised rooms on one server, and paired servers that
can see each other's presence and admit remote guests under a host-set cap
([ADR 0044](docs/adr/0044-server-identity-and-peering.md) through
[ADR 0047](docs/adr/0047-remote-streaming-is-capped-by-the-host.md)). Only the host
needs to be reachable: a friend's server says who is there by itself
([ADR 0103](docs/adr/0103-a-friend-who-cannot-be-reached-says-who-is-there.md)).

**Plugins.** Signed `.lcplugin` bundles running as WebAssembly under wazero,
deny-by-default capabilities, and a two-step install that grants them
explicitly ([ADR 0020](docs/adr/0020-plugin-isolation-boundary.md),
[ADR 0021](docs/adr/0021-plugin-distribution-and-trust.md)). Writing one is
[docs/writing-a-plugin.md](docs/writing-a-plugin.md), which is honest about the
two things a third party still cannot do.

**Running it.** A dashboard of the last two days
([ADR 0106](docs/adr/0106-the-dashboard-remembers-two-days.md)), metrics for
Prometheus ([ADR 0096](docs/adr/0096-metrics-have-their-own-key.md)),
webhooks ([ADR 0097](docs/adr/0097-a-webhook-goes-where-the-owner-points-it.md)),
scheduled backups with restore from Settings
([ADR 0058](docs/adr/0058-a-backup-is-the-database.md)), live updates to every
open screen ([ADR 0079](docs/adr/0079-the-server-says-when-something-changed.md)),
and **Report an issue**, a form whose report leaves the server only when you
press Send ([ADR 0109](docs/adr/0109-a-report-goes-only-when-sent.md)).

**Its own window.** `LANcast` opens a WebView2 window rather than handing
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
go test ./... && go run ./cmd/lancast serve -addr :8080
```

Then open <http://localhost:8080>, create the first account, add a library
pointing at a folder of media, and scan it.

| Flag | Default | Meaning |
|---|---|---|
| `-addr` | `:8080` | Listen address |
| `-data` | `%APPDATA%/LANcast` (platform config dir) | Where `lancast.db` lives |
| `-v` | off | Verbose logging |

Build the one LANcast executable (the app, and the server as `LANcast serve`):

```bash
./build.sh
```

The client UI is a React + TypeScript app in `web/`, built by Vite and committed
into `internal/web/dist` so a built server is genuinely one file. After changing
it, run `npm --prefix web test` and `npm --prefix web run build`, and commit the
rebuilt `dist`.

### Metadata

**Nothing to sign up for.** A release comes with its own
[TMDB](https://www.themoviedb.org) key, for posters, synopses, cast and artwork,
and its own [OpenSubtitles](https://www.opensubtitles.com) key, for subtitle
search ([ADR 0110](docs/adr/0110-lancast-carries-its-own-provider-keys.md)).
Settings › Metadata says which key each one is using.

**Your own key always wins.** Enter one under Settings and it is used instead.
For TMDB, use the **API Key (v3 auth)**, the 32-character hex value; the API Read
Access Token beside it is a v4 bearer JWT and will be rejected. An optional
opensubtitles.com login raises how many subtitles you can download a day. Keys
and the login are stored `0600` outside the database and are never readable
back through the API.

**You can turn the built-in keys off.** With *Use LANcast's built-in keys* off
and no key of your own, LANcast falls back to filenames and Kodi-style `.nfo`
sidecars, contacts neither service, and never reports the missing key as an
error. That is the no-phone-home principle being real rather than decorative.
A build from source carries no built-in keys and behaves this way until you
enter one.

> This product uses the TMDB API but is not endorsed or certified by
> [TMDB](https://www.themoviedb.org). Attribution is a condition of their free
> tier and is also shown in Settings › About.

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

Releases ship one executable — `LANcast.exe` on Windows, `lancast` on Linux —
with no runtime to install. Opened, it is the app, with its window and tray
icon; installed as a service, the same file is the server
([ADR 0080](docs/adr/0080-one-executable-two-roles.md)). On Windows the
installer registers the service and adds a shortcut; on Linux/NAS it runs as a
systemd service. Full steps, including the one
data-directory rule that matters most, are in
**[docs/install.md](docs/install.md)**.

The server can check for, download, verify, and stage an update that swaps
itself in on the way down. It swaps `LANcast.exe` and the libraries beside it;
a change to the installer itself needs the installer.

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
| [docs/adr/](docs/adr/) | 111 decision records — why things are the way they are |

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

**Once it binds the network, it serves HTTPS.** Point it at a certificate you
already have (Settings › Server › Network) or let LANcast generate a self-signed
one — either way the password and session cookie are encrypted on the LAN. A
self-signed cert shows a browser warning until you trust it; the LANcast window
pins the key instead and does not.

**Reaching it from outside is your decision, and off by default.** The simplest
safe route is still a VPN that puts your device on the LAN (Tailscale,
WireGuard). If you do expose it, give it a public name: LANcast can then request
a publicly trusted certificate for that name — only when an administrator turns
it on ([ADR 0081](docs/adr/0081-a-public-name-can-earn-a-public-certificate.md))
— and accounts can add passkeys
([ADR 0082](docs/adr/0082-a-passkey-belongs-to-the-public-name.md)). A bare
self-signed certificate cannot prove the server's identity to the public
internet, so do not port-forward one. See [docs/security.md](docs/security.md).

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
