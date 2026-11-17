# Roku app — plan

**Status: proposed, 2026-11-17.** The first item on the 1.x roadmap. Nothing here is built, and nothing gets built until step 0's spikes have run on a real Roku.

## What it is

A LANcast app for Roku TVs and sticks, built as a thin client against the documented API, the same as any third-party client (README: *clients are thin*). It is the first client that LANcast does not draw in a browser engine. That makes it the first real test of whether `docs/api.md` is enough to build a client from.

**Why Roku first:** it is the TV in the theater room (display 3, a Roku TV), and a phone or a browser on a TV is the weakest way to use LANcast today. Sitting 1 found the same gap from the other side: a pad cannot move around the app.

## What the server already has

Most of what a TV app needs exists and is documented:

| Need | Already there |
| --- | --- |
| Signing in without a cookie | API keys, presented as `Authorization: Bearer` (ADR 0061). A key never reaches an admin route, which suits a TV |
| Telling the server what the device plays | `?profile=` plus codec claims (`hevc10`, `high10`, `matroska`…) on `/playback` and the stream routes |
| A stream a TV can play | HLS (fMP4 segments) for remux and transcode, and the file itself for direct play |
| Resume and watched | `PUT /api/items/{id}/progress` |
| Home rows | Continue Watching, New this week, pinned shelves (Homepage 2.0) |
| Skip intro and credits | Markers on the item |
| Finding the server | DLNA's SSDP announcement, when DLNA is on (ADR 0086) |

## What is missing on the server (two API changes, so an ADR)

1. **Pairing a TV.** Typing a password, or worse an API key, with a remote is the worst part of every TV app. Proposed is the usual device flow:
   1. The TV asks the server for a short code.
   2. It shows the code, and the person enters it in LANcast on a phone or computer, already signed in.
   3. They approve it, and the TV's next poll receives an API key for that account, named after the device.

   It is two or three new routes plus one screen in the web client. Every rule that applies to keys stays: hashed at rest, never reaching an admin route, and revocable in Settings, where the TV shows up by name.
2. **A Roku profile.** `tv` claims too much for a Roku: TrueHD and DTS are passthrough at best and absent at worst, and VP9 and AV1 depend on the model. The proposal is a conservative `roku` floor (H.264 and AAC, plus AC3 and EAC3 passthrough) that the app widens with the claims it **asks the device about**. Roku can answer per codec, profile and level, which is the same shape as the claims the browser client sends after probing. The `knownCapabilities` set stays closed.

Both changes go in `docs/api.md` and `docs/openapi.json` in the same commit, as always.

## Scope of the first version

**In:**
- finding the server (typing an address, or SSDP when DLNA is on);
- pairing;
- home rows;
- browsing a film or show library;
- a film's or show's page;
- playing a film or episode: resume, progress and watched, subtitles, choosing the audio track, skip intro and credits, and Up Next.

**Out, for later versions:** music, photos, comics, audiobooks, games, Watch Together, play on another device, friends' servers, and settings. Live TV stays parked.

## Steps

**0. Spikes, on the theater Roku, before anything is committed**
- **a. Roku Media Player against LANcast's DLNA**, which already exists. It shows what a Roku gets today with no app, and it is free.
- **b. A throwaway sideloaded app** (developer mode) that plays one LANcast HLS stream with a `Bearer` header:
  - over the public name, which has a real certificate;
  - over the LAN address, with LANcast's self-signed certificate;
  - and that seeks within it.

  The self-signed case is the open question. The app has to trust a certificate it learns at pairing, the way the desktop window and paired servers pin the server's key ([security.md](security.md), ADR 0124). If Roku cannot do that for its video player, LAN use needs either the public name or a certificate the TV already trusts. That changes the design, which is why it goes first.
- **c. Ask the Roku what it decodes** (H.264 and HEVC profiles and levels, AC3, EAC3, DTS, AAC), and write down the answers. They become the `roku` floor.

**1. ADR** — the Roku app, device pairing and the `roku` profile, decided with step 0's results in hand. It also covers where the app lives: proposed is `clients/roku/` in this repository, so an API change and the client that needs it land in one PR.

**2. Server: pairing routes, the `roku` profile, and the approval screen.** Go tests for the flow (expiry, single use, rate limit) and for the profile's decisions, written in the `decide_test.go` style: one test per codec and container, with its reason.

**3. App skeleton:** the address, pairing, a signed-in home screen, and remote focus that follows design.md, where gold means only *where you are*.

**4. Browse and detail:** library grids, a film page, and a show's seasons and episodes.

**5. Player:** HLS and direct play, resume, progress, subtitles, audio tracks, markers and Up Next.

**6. Proof on the real TV, against the installed service:**
- a film down each path (direct, remux, transcode);
- an episode into Up Next;
- a show with subtitles;
- the server restarted mid-film;
- the TV's key revoked mid-session.

**7. Distribution.** Sideloading (developer mode) covers Chris's own TVs and needs nothing from Roku. Anyone else needs the app published through Roku's developer program, which means certification and a review. Roku has also changed how unlisted or "beta" apps work more than once, so check the current rules when this step comes. Decide here whether a public listing is worth it.

## Build and test

BrightScript and SceneGraph. A compiler and linter that runs from npm (BrighterScript) gives CI a `Roku` job that fails on errors, beside Go and Client. The app's logic that isn't UI (claims, the pairing poll, progress reporting) is written so it can be tested without a TV. What the app looks like on screen is still only known by looking at the TV, as with the web client.

## Risks

- **TLS on the LAN** (step 0b). The biggest unknown.
- **Roku's HLS player and LANcast's segments:** fMP4, the growing playlist during a remux, and seeking past what has been converted. The remux-patience and `-copyts` lessons are the ones to re-check here.
- **Store review** may ask for things a self-hosted app cannot have, such as a demo server for reviewers. That's for step 7, and it doesn't block sideloading.
- **Effort:** a new language and a new UI toolkit. Steps 3–5 are most of the work. Step 0 is a day or two, and it decides whether the rest is weeks or something smaller.
