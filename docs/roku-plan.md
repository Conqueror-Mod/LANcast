# Roku app — plan

**Status: steps 0–6 built and proven on the TV (2026-11-19); step 7 decided: aim for the Roku Channel Store.** The first item on the 1.x roadmap.

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

### Step 0 results (2026-11-17, a 55" Roku Select Series 4K TV, 55R4AX, Roku OS 16.0.4)

**0b. Playback, through a throwaway sideloaded app with a `Bearer` key:**

| Test | Picture | Sound |
| --- | --- | --- |
| Public name, the file played directly | yes, 2 s to the first picture | yes |
| Public name, transcode (HLS) | yes, 5 s | **no** |
| Public name, remux (HLS) | yes, 8 s | **no** |
| LAN, pinned certificate, direct and transcode | yes, 0–1 s | direct yes, transcode **no** |
| LAN, Roku's own CAs only | refused: `SSL peer certificate … was not OK` | — |

- **TLS on the LAN is solved by pinning.** The video player trusts LANcast's self-signed certificate when the app hands it that certificate (`setCertificatesFile`). It refuses the same server without it, so the check is real. Pairing can carry the certificate, as the desktop window's pin does. Seeking worked on every path.
- **HLS sound is the fault, and it is the packaging.** A lab of the same 90 s clip (H.264 + AAC) packaged four ways:

  | Packaging | Sound |
  | --- | --- |
  | fMP4, audio and video in the same segments, no master playlist (**what LANcast serves**) | no |
  | the same, with a master playlist naming the codecs | no |
  | MPEG-TS, audio and video together | yes |
  | fMP4 with audio as a separate rendition (`EXT-X-MEDIA`) | yes |

  A master playlist wrapped around LANcast's real transcode was silent too. **This Roku does not play audio interleaved in fMP4 HLS segments.** For a Roku, the server has to send MPEG-TS segments or separate fMP4 audio. Choosing between them is the ADR's first decision. Separate audio is the more modern of the two, and it also gives the player its own audio-track list. MPEG-TS is the smaller change, but HEVC in TS is less certain.

**0c. What the device says it decodes** (`roDeviceInfo`):
- **Video:** H.264 High up to level **4.1**, but not 5.1 and not High 10. HEVC Main and **Main 10** (in MP4 and MKV). MPEG-2.
- **Audio:** AAC, MP3, AC3, EAC3, FLAC, ALAC, Opus, Vorbis and LPCM, but **not DTS or TrueHD**.
- **Not yet known:** VP9 and AV1. They were asked about in the wrong containers, so ask again before the profile is written.

This is the `roku` floor's first draft: wider than `browser` on HEVC and Dolby audio, and narrower than `tv` on DTS, TrueHD and H.264 5.1.

**0a. Roku Media Player over LANcast's DLNA server** (discovered without help; DLNA always sends the file as it is):

| File | Result |
|---|---|
| MP4, H.264 + AAC stereo | Plays with sound |
| MKV, HEVC + **DTS** 5.1 by default (also TrueHD and AC3 tracks) | Picture, **no sound**, as expected with no DTS decoder. DLNA cannot choose the AC3 track. |
| MKV, H.264 + **AAC** stereo, one audio track | Picture, **no sound**. The decoder list does not explain this one. |

In all three, seeking works, but playback **stays paused after a seek** until Play is pressed.

What this means: DLNA is a stopgap at best, not a substitute for the app. It cannot transcode or choose a track, and it loses the sound even on a file whose codecs the TV says it decodes. The silent AAC-in-MKV file is still unexplained. Before the `roku` profile marks MKV as direct-playable, play that file directly in the sideloaded app; until that passes, treat MKV as remux.

**1. ADR** — [ADR 0125](adr/0125-a-tv-pairs-by-code.md), accepted 2026-11-18: the Roku app, device pairing and the `roku` profile, decided with step 0's results in hand. It also covers where the app lives: proposed is `clients/roku/` in this repository, so an API change and the client that needs it land in one PR.

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

### Step 6 results (2026-11-19, app build 10, the server from main at 2839418f swapped into the installed service)

All eight checks pass:

- **Direct:** 21 Jump Street and 2012.
- **Remux:** 28 Years Later.
- **Transcode** (DTS re-encoded to AAC): Army of Darkness and 2 Fast 2 Furious. 2 Fast 2 Furious went black on the spike before #1178.
- **Up Next.**
- **Subtitles on a show.**
- **Server restart mid-film.**
- **Key revoke, then re-pairing.**

Two faults were found and fixed on the way:

- **Server restarted mid-film.** Roku reports a stream it gives up on as `finished`, so the app marked the film watched, and the next Play started at 0:00. The app now trusts a finish only within a minute of the end. Otherwise it keeps the last place it was really playing and says the stream stopped.
- **Key revoked during playback.** A progress save never read its answer, and a refused HLS segment only looked like a playback error. Both now reach the 401 handling, and the TV goes to pairing.

Still owed: 21 Jump Street's 24p judder on direct play, and switching the audio track on a title with more than one.

**7. Distribution.** Sideloading (developer mode) covers Chris's own TVs and needs nothing from Roku. Anyone else needs the app published through Roku's developer program, which means certification and a review. Roku has also changed how unlisted or "beta" apps work more than once, so check the current rules when this step comes. Decide here whether a public listing is worth it.

### Step 7 decision (2026-11-19): aim for the Roku Channel Store

**The rules as checked on 2026-11-19.** Roku removed private, uncertified apps some years ago. Three ways are left:

- **Sideloading** needs nothing from Roku. A TV holds one sideloaded app, and it never expires.
- **A beta channel** installs from a private link with no review. It takes up to 20 testers per channel and 10 channels per account. **It is deleted, and disabled on every TV, 120 days after it was created.** Good for a friend test, but no way to ship.
- **A public listing** passes certification on every submission.

Self-hosted servers do get listed: Jellyfin and Emby are both in the store.

Chosen: the store. Sideloading stays the way to develop, and a beta channel is the way to test with friends before each submission.

**The certification criteria that bite** ([criteria](https://developer.roku.com/docs/developer-program/certification/certification.md), [Spring 2026 update](https://blog.roku.com/developer/channel-certification-criteria-updates-april-2026)). Re-read the current list when each item starts, because Roku revises it twice a year.

1. **Trick-play thumbnails** for anything over 15 minutes ([trick mode](https://developer.roku.com/docs/developer-program/media-playback/trick-mode/trick-mode.md)), as BIF or as HLS image playlists. LANcast already makes scrub-bar preview sheets (ADR 0105). The work is a server route that serves them in a form Roku reads. BIF covers direct play and HLS with one format. It is an API change, so it goes in `docs/api.md` and `docs/openapi.json` with a test. Previews are off by default (`scrub_previews`), so the app has to play normally without them.
2. **Deep linking** for every media type (5.1). The app is launched with `contentId` and `mediaType`, or receives them while running through `roInput`, and goes straight to the item. **Direct to Play** for voice commands (5.2): "play X" starts playback with no page in between. `contentId` is the LANcast item id.
3. **Instant Resume** (US, required from 2026-10-01): the app comes back where it was after the TV switches away.
4. **Performance:** time to launch, time to first frame, and remote response. Measure them on the TV against Roku's published budget before submitting. Pairing and the first Home load are the slow parts to watch.
5. **The rest of the checklist:** artwork (icons and splash at Roku's sizes), a non-zero version, captions (done), and Back always leaving cleanly.

**Reviewer access, the real cost.** A reviewer needs something to sign in to. The plan:

- A **reviewer account** on the public server (`thelighthouse.dynv6.net`, a real certificate).
- It is limited by ADR 0084's hidden libraries to **one demo library** of openly licensed films, for example the Blender Foundation's CC-BY films. None of the private library is shown.
- It is not an administrator.
- The submission notes give the server address and the account's password for the web page where the TV's code is approved.
- The reviewer account is deleted or disabled between reviews. It is the one standing public login this step creates, so it is kept as small as possible.

Before submitting, check that the demo library shows every path (direct, remux, transcode), subtitles, markers and Up Next.

**What is Chris's alone:**

- the Roku developer account, which needs his identity and agreement to Roku's terms;
- the store listing's text and artwork;
- a privacy statement URL: LANcast collects nothing (no phone-home), and the statement says so;
- choosing and loading the demo films;
- pressing Submit.

**Order:**

1. trick-play thumbnails (server + app);
2. deep linking and Direct to Play;
3. Instant Resume;
4. a performance pass on the TV;
5. artwork and the listing;
6. the demo library and reviewer account;
7. a beta channel round with a friend's TV;
8. submit.

Items 1–4 are code and can be proven on the sideloaded app the way steps 3–6 were.

## Build and test

BrightScript and SceneGraph. A compiler and linter that runs from npm (BrighterScript) gives CI a `Roku` job that fails on errors, beside Go and Client. The app's logic that isn't UI (claims, the pairing poll, progress reporting) is written so it can be tested without a TV. What the app looks like on screen is still only known by looking at the TV, as with the web client.

## Risks

- **TLS on the LAN** (step 0b). The biggest unknown.
- **Roku's HLS player and LANcast's segments:** fMP4, the growing playlist during a remux, and seeking past what has been converted. The remux-patience and `-copyts` lessons are the ones to re-check here.
- **Store review** may ask for things a self-hosted app cannot have, such as a demo server for reviewers. That's for step 7, and it doesn't block sideloading.
- **Effort:** a new language and a new UI toolkit. Steps 3–5 are most of the work. Step 0 is a day or two, and it decides whether the rest is weeks or something smaller.
