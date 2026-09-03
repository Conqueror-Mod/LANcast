# Immich comparison study

Date: 2026-09-03 · Immich at 113k stars, AGPL-3.0, TypeScript/NestJS + Postgres +
Redis + Python ML, Flutter mobile, SvelteKit web.

This is a study, not a plan. Nothing here is decided. Where a difference is a
decision LANcast already made on purpose, it is recorded as such rather than as
a gap.

## What Immich actually is

"High performance self-hosted photo and video management solution" — a Google
Photos replacement. It is **not a peer product**. Immich owns an upload pipeline
and treats the filesystem as an implementation detail; LANcast never owns the
files and treats the filesystem as the truth. Most of Immich's surface (mobile
auto-backup, storage templates, upload dedup, trash) exists to solve *ingestion*,
which LANcast does not have and should not acquire.

The genuine overlap is one library type: **Pictures** (ADR 0028) — plus a set of
platform concerns (jobs, integrity, backup/restore, plugins, search) where the
same problem is being solved twice and the two answers can be compared.

---

## Where LANcast is doing it right

These are worth stating because Immich is the larger project and it is easy to
read size as correctness.

**Filesystem-as-truth beats an owned upload directory.** Immich's own docs are
full of the cost of the other choice: a danger box telling users never to touch
files in their own upload folder — "Do not touch the files inside these folders
under any circumstances" — a whole *System Integrity* subsystem to find untracked
files, missing files and checksum drift, and a warning that editing a file on
disk produces a checksum mismatch whose only supported fix is delete and
re-upload. LANcast's "scanning marks missing, never deletes" and containment
re-verification are cheaper guarantees, and they exist because the server never
claimed to own the bytes.

**External libraries are a bolt-on for them and the core for us.** Immich's
external library page carries four separate caveats: metadata added in Immich is
lost if you move the file (a known issue), aggressive caching means a refreshed
asset needs a browser hard-reload, file watching is experimental and does not
work on network drives, and a library can only ever belong to one user. Every one
of those is a symptom of the mode being secondary. Ours is the only mode.

**Not reading GPS is a stronger position than reading it.** ADR 0028 refuses it
and `internal/photo/exif.go` has no parser for it at all, which is a real
guarantee rather than a policy. Immich reads GPS, reverse-geocodes it locally
against GeoNames, and puts a global map in the product. That is a legitimate
choice for a photo app and the wrong one for us — but note their geocoding *is*
fully local, so "we have a map" would not by itself violate no-phone-home. It
would violate ADR 0028, which is a different and more deliberate objection.

**Single-binary, single-process, embedded database.** Immich needs Postgres +
Redis + a Python ML container + optional split worker containers, and its docs
spend real length on `ENOSPC` inotify limits, container mount permissions, and
volume-mount `.immich` marker files. Our deployment story is one exe. ADR 0001
looks better after reading their install docs, not worse.

**Verification discipline.** Immich's e2e suite is genuinely good and covers
things ours does not (auth/authz, thumbnail generation, library scanning against
a live production-shaped stack). But nothing in their docs corresponds to our
session-0 rule — the "verify ffmpeg changes as the installed service" section in
CLAUDE.md is a discipline they do not appear to have, and their hardware
transcoding page is candid that the feature "is still experimental and may not
work on all systems". We paid for that lesson once and wrote it down.

**Plugin isolation was the right call, and they agree.** Immich shipped a plugin
system (`plugin.service.ts`, `packages/plugin-core`, `packages/plugin-sdk`) that
compiles to **WASM via Extism**. ADR 0020's wazero sandbox is the same bet,
reached independently. See below for the one thing they did better.

---

## Where Immich is ahead — things worth taking

Ordered roughly by value-to-effort for LANcast.

### 1. A plugin SDK, not just a plugin ABI

Immich's `plugin-core` is written in **TypeScript** and built to WASM with
`extism-js`. Plugin authors write TS against `@immich/plugin-sdk` and never touch
a WASM toolchain. LANcast has the harder half — the sandbox, the capability
model, the signing and trust layer (ADR 0021), which is more than Immich
documents — but an author currently has to bring their own WASM-targeting
language. A first-party SDK in one ergonomic language is the difference between a
plugin *system* and a plugin *ecosystem*. Highest leverage item here, because the
hard part is already built.

### 2. Workflows / automation

`workflow.service.ts` and `workflow-execution.service.ts` are a user-facing
automation engine sitting on top of the plugin runtime. We have jobs; we have no
way for a user to say "when X, do Y". Flagging it as a shape that exists, not a
feature to copy — it wants understanding before it wants adopting.

### 3. Integrity checking as a first-class subsystem

Immich runs nightly checks for untracked files, missing files and **checksum
mismatches**, with time and progress budgets so a full checksum pass can spread
over several days, and a maintenance page that reports findings and can re-check
only what was previously reported. LANcast has scan issues and a review queue but
no periodic "is the library still intact" pass. The checksum half matters less
for us — we do not own the bytes — but the **budgeted, resumable, reportable
nightly job** pattern generalises. It is what a "verify every artwork and
thumbnail still resolves" job would want to look like.

### 4. Restore-from-backup as a supported first-run path

Relevant *right now*, given the `backup-api` branch. Immich has: scheduled dumps
with retention (default: keep 14, daily at 2am); a **restore point taken before a
restore**, with automatic rollback if the restore fails; a health check after the
restore; version-compatibility indicators on each backup in the list (matches /
different version / unknown); a **maintenance mode** with its own separate login
whose URL the server prints to the log; and a "Restore from backup" option on the
*onboarding* screen of a fresh install. ADR 0058 covers "a backup is the
database"; the operational envelope around it is where their design has more in
it than ours. Restore-point-with-rollback and the version indicator are both
cheap and both prevent a class of unrecoverable user error.

### 5. Semantic search (CLIP) — as a plugin, if at all

Immich's search is the feature users name first: freeform natural-language search
over image content via CLIP embeddings in Postgres/VectorChord, plus OCR text
search, plus structured filters (camera make/model/lens, star rating, date range,
path or folder substring, tags, people, in-album / not-in-any-album). LANcast's
`?q=` is free text over title and series — right for films and albums, close to
useless for a 40,000-photo library where the filename is a UUID.

This is a heavy feature: an ONNX runtime, a model download, a vector index. It
lands squarely on M4's plugin boundary, and it is the strongest argument for item
1 — **semantic photo search is exactly the kind of thing that should be a plugin
and not a feature**. We already ship the precedent: faces run in a native sidecar
with a model install flow (ADR 0052, `/api/faces/models/install`), so
"download a model, run it out of process" exists.

### 6. Tags, favourites, ratings on anything

We have ratings. We have no tags and no favourites on media items — the only
"favourite" in the codebase is per-device channel pinning in ADR 0039. Immich has
hierarchical tags read from XMP `TagsList` / IPTC `Keywords` and written back to
sidecars. Two observations:

- **Tags are a locked-field problem, and ours is already solved.** ADR 0008's
  field-level locking is the mechanism a user-tag system needs; the design work
  is largely done.
- **Favourites collide with the design system.** `docs/design.md` says gold means
  where-you-are and nothing else, and explicitly names "favorite" as a thing gold
  must never mean. A favourites feature needs a non-gold affordance decided up
  front, or it quietly kills the focus signal.

### 7. Duplicate detection with a review utility

Immich detects visually-similar assets, groups them, and gives a review page that
preselects which to keep (larger file, more EXIF) and **merges metadata from the
discarded copies into the kept one**: albums, favourite, highest rating, combined
descriptions, merged tags, most-restrictive visibility, location only if all
copies agree. We have `/api/collisions`, which is the identity-collision case,
not the same-content case. ADR 0042 (two files, one work) and ADR 0049 (an
edition is a copy of a work) already circle this territory. Their *metadata merge
on resolve* table is the part worth stealing outright — it is the bit everyone
gets wrong.

### 8. API keys

Immich has per-user API keys with a management UI, and its CLI authenticates with
one. LANcast has server sessions and nothing else, and the third-party client
story is currently awkward enough to need a memory note (pin the SPKI, let the
first GET upgrade you to https, or the Secure cookie vanishes). An API key would
make that straightforwardly documentable. Small, well-understood, and unblocks
any future scripting or CLI story.

### 9. Public share links

Random-URL links with optional expiry, optional password, and per-link permission
scoping. LANcast's sharing model is accounts, peers and remote guests (ADRs
0044–0047) — architecturally richer, but there is no "send my sister this one
thing" path. Worth an ADR to decide *whether*: the link is served by our own
server so no-phone-home survives, but the attack-surface question is real given
an unauthenticated route into a server that also exposes filesystem browsing.

### 10. Smaller items, listed without argument

- **An OpenAPI spec generating typed clients** for every client (`open-api/`,
  `@immich/sdk`). Our `docs/api.md` is hand-maintained prose, and the CLAUDE.md
  rule about it exists *because* it drifts. A generated spec makes drift
  impossible rather than forbidden. This is the one item here about our process
  rather than a user feature, and probably the most valuable of the ten.
- **i18n** — 89 locales. We have none. Not urgent; worth not painting into a
  corner.
- **Prometheus metrics and structured JSON logging**, both opt-in. We have
  `/api/activity`, `/api/logs` and an audit log, but no metrics endpoint.
- **Memories** ("x years ago"). Cheap — `taken_at` is already indexed.
- **Stacked photos** — burst and RAW+JPEG grouping. Overlaps ADR 0049.
- **Non-destructive editing** (crop, rotate, mirror), original preserved and
  still downloadable. The non-destructive framing is the interesting part.
- **Per-library exclusion patterns** (glob, matched against the full path). We
  ignore by extension only; a user with a `Raw/` folder or a Synology `@eaDir`
  has no recourse. Cheap and frequently wanted.
- **Configurable scan interval as a cron expression**, per library.
- **Split workers by environment variable**, for moving transcoding off the API
  box. Contradicts single-binary; noted, not recommended.

---

## Things Immich does that we should not take

- **Owning the files.** Storage templates, upload directories, and the whole
  integrity subsystem that follows from them. Covered above.
- **Mobile auto-backup.** No corresponding user need.
- **Chromecast.** Their own docs say it needs the instance publicly reachable
  over HTTPS with a DNS record resolvable by Google's nameservers, and it loads
  Google-hosted scripts at page load. That is a phone-home in all but name, and
  they made it opt-in for exactly that reason. If casting is ever wanted, it
  wants a protocol that does not route through a third party.
- **Redis + Postgres + a separate ML container**, and a separate microservices
  container. ADR 0001 stands.

---

## What I would look at first

1. **Generated OpenAPI spec** — kills the documentation-drift failure mode that
   CLAUDE.md calls the most damaging one in the project.
2. **Plugin SDK in one ergonomic language** — the sandbox and trust model are
   built; this is what makes them usable by somebody else.
3. **Restore hardening** — restore point, rollback, version indicator, while the
   backup work is still open on this branch.
4. **API keys** — small, unblocks third-party clients and any future CLI.
5. **Tags** — the locking mechanism already exists; decide the favourites
   affordance at the same time so gold stays intact.

Semantic search is the big one, and it should be the thing that *proves* the
plugin SDK rather than the thing that gets built into the server.
