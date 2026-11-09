# LANcast 1.0: what the number promises, and what stands between here and it

Date: 2026-11-08 · Status: **accepted** 2026-11-08, every decision in §7
taking its recommendation (§8) ·
Main is v0.10.10, schema 91, with Settings 3.0 steps 1–2 unreleased

## 1. What 1.0 means

LANcast does not need another feature to be 1.0. All four milestones are
built, and what the roadmap lists as left is breadth: it will still be breadth
in a year, and waiting for it to run out means never shipping 1.0. A version
number is a promise to the person installing it. Three parts of that promise
are not yet true enough to make:

1. **What it says works has been seen working.** The roadmap's *Built, not
   yet watched working* list has eleven entries. At 1.0 each one is either
   watched, or named in the notes and the docs as unproven. Nothing goes out
   under 1.0 as a claim nobody has checked.
2. **A stranger can get it and keep it current.** A clean install on a machine
   that has never had LANcast, the in-app update from the oldest version that
   supports it, and the Linux archive the README offers, each run once on the
   published artifacts.
3. **The documents describe 1.0 to someone new.** README, install, security
   and privacy read as a description of the product, not as a diff against 0.10.

**Proposed definition:** 1.0 is scope frozen after Settings 3.0 and Profile
2.0, the ledger empty or labelled, the install and update paths proven on real
artifacts, and the documents written for a newcomer.

It is **not** parity with Plex, a phone or TV client, live TV, or a schema
freeze. Migrations are already forward-only, every upgrade is already one-way,
and 1.0 changes neither.

**1.0 does not change the API.** `/api` is permanently version 1
([ADR 0018](adr/0018-api-contract-and-versioning.md)). The app's 1.0 and the
API's v1 are unrelated numbers. The release notes should say so once, because
a third-party client author will ask.

## 2. Where it stands (updated 2026-11-09)

- **Released:** v0.10.11, which finished both features in flight: Settings 3.0
  steps 3–7 and Profile Page 2.0 ([plan](profile-2-plan.md)). Schema 95.
  Section 3A is therefore done, and the owner's last notes (§9) are all that
  stand before the freeze.
- **Open PRs:** the parked uncomposed-video plan document (#984).
- **Open faults:** two, each waiting on a reproduction now that the Debug log
  holds about 28 hours: music stalling on a phone, and a sign-in refused at the
  public name.
- 123 decision records, about 3,700 Go tests and 2,000 client tests.

## 3. The work between here and 1.0

### A. Finish the two features in flight, then freeze scope

- **Settings 3.0**, steps 3–7 ([plan](settings-3-plan.md) §5). Step 3 finishes
  what Homepage 2.0 started. Steps 4 and 5 give the newer kinds and the
  metadata language somewhere to be set. Step 6 is a schema change with an
  ADR. Step 7 is read-only.
- **Profile Page 2.0**, from its plan once that plan is written and decided.
- **Then scope freezes.** New features wait for 1.1, and sections B–F are the
  only work. A fault found during the freeze is fixed. A wish found during it
  goes in the backlog.

*Recommendation:* all seven Settings steps are in, because they are decided and
each is small. If the freeze needs to come sooner, step 7 (the storage overview)
is the one to move to 1.1, since nothing depends on it.

### B. Empty the ledger

Each entry is from the roadmap's *Built, not yet watched working* list. **Watch**
means a person sees it work on the installed service. **Label** means it ships
named as unproven in the release notes and the relevant document.

| # | Entry | What watching it takes | Recommendation |
|---|---|---|---|
| 1 | **MusicBrainz albums**, first pass over the real library (ADR 0115) | Read the match states in a copy of the live database; spot-check the review and unmatched rows | Watch. Read-only, one session, needs nobody |
| 2 | **Audiobooks** heard with sound, as the service (ADR 0116) | Play a book across a chapter change and let the sleep timer fade | Watch |
| 3 | **Extras**: an MKV extra in the desktop window (#950) | One click | Watch |
| 4 | **Watch Together between servers on different networks**, retested with both servers at v0.10.7 or later | Georgia, one evening | Watch. **The most valuable single check here.** Georgia's move (2026-11-01) made "different networks" testable for the first time. The 2026-11-07 attempt passed posters and found the eight-minute start, fixed in v0.10.7 (#1024) |
| 5 | **Roster push from another network** (ADR 0103), **a friend's join request over a full-screen film** (v0.10.6), and **an invite link opened on a phone** (ADR 0104) | The same evening as 4 | Watch |
| 6 | **The follower side of Watch Together on one server** | Two windows signed in to one server | Watch |
| 7 | **Retro**: Game Boy and Game Boy Color on real dumps, a pad on the TV, touch controls on a real phone; Master System | A Master System dump, which the test library lacks | Watch GB, GBC, the pad and touch. Label Master System unless a dump turns up |
| 8 | **Now Playing and the visualizers** in the desktop window and on the TV with a pad; synced lyrics on a real track; the v0.9.93 MilkDrop fixes on the phone that found them | Desktop, TV, phone | Watch |
| 9 | **Skip credits on a film the server converts**, in a browser; **the remux path** on a client that claims no Matroska; **the denial reset row's** appearance | A browser tab against the service | Watch |
| 10 | **Trusted proxies** behind a real Caddy or nginx, **metrics** in a real Prometheus, **webhooks** into a real Home Assistant | Software that may not be running here | Label as "checked with simulated clients", unless one of them is already running |
| 11 | **Theater HDR and passthrough**, and **live TV** | Cables; a decision to unpark | Label. Both are parked by choice (§5) |

Group the watched entries into two or three sessions. The
[drive-lancast](../.claude/skills/drive-lancast) skill does the driving where it
can, so the owner's part is only what needs a person: sound, a phone, a pad,
and a friend.

### C. Close or label the two open faults

- **Music stalling on a phone.** Reproduce it once and copy the log at once.
  Read the `/api/stream` request times and the "music on deck/element" lines.
- **A sign-in refused at the public name.** The same. `login refused` with
  `method=passkey` now logs the reason at WARN.

If either has not come back by the end of the freeze, it goes into the 1.0
notes under known issues as *reported once, not reproduced*. That is a true
statement. Holding 1.0 for a fault nobody can make happen is not useful.

### D. Prove getting it and keeping it

1. **A clean Windows install** from `LANcast-Setup-<version>.exe`, on a machine
   that has never had LANcast (Windows Sandbox is enough). Run it through first
   run, the ffmpeg fetch ([ADR 0048](adr/0048-media-tools-install-themselves-on-first-run.md)),
   the first account, a library and a played film. Test the installer, not the
   bundle: v0.9.29 crashed machines on the installer's finish page while every
   check on the bundle passed.
2. **The in-app update from the oldest version that supports it.** Every
   release note says 0.9.79 or later updates in place. Install 0.9.79 and
   update straight to the candidate, then do the common case: the previous
   release to the candidate. The Areas table still says the updater is
   "proven as far as staging". It has been the routine path since v0.9.79, but
   no single run from the oldest supported version has been recorded.
3. **The step from 0.x to 1.0.** `update.Newer` compares numerically, so
   0.10.x to 1.0.0 counts as newer. A unit test case for exactly that pair
   costs nothing and should exist before the tag. `parseVersion` rejects a
   suffix, so a `v1.0.0-rc.1` build could never update itself to 1.0.0.
   **Release candidates therefore go through the existing test-build swap kit,
   not through tags.**
4. **Linux.** The README offers `lancast` for Linux on amd64 and arm64. The
   release workflow only starts it and reads `/api/health`. Run the amd64
   archive once under systemd (WSL2 with systemd will do), through a scan and a
   film played with ffmpeg. arm64 is labelled *built, not run* unless a Pi is to
   hand. Whether Linux is supported at all in 1.0 is decision 4.

### E. Security, before strangers run it

- **ffmpeg runs as LocalSystem.** ffmpeg parses untrusted media on a service
  that can face the internet, so a parser bug there runs as SYSTEM. Find out
  what it costs to give the child processes a restricted token or a
  low-privilege account. If that is more than a few days of work, security.md
  says so plainly for 1.0, and it is built in 1.1 (decision 5).
- **The pinned ffmpeg has no update path.** Check the pin's age and what an
  update would touch before building anything. At minimum, the 1.0 notes name
  the ffmpeg version shipped.
- **Read security.md against the code** for everything added since it was last
  revised: guest tickets, invite links, play on another device, webhooks, the
  metrics key, built-in provider keys.

### F. Documents for a newcomer

- **README, install.md, security.md, privacy.md**, re-read as a stranger would
  read them. privacy.md already covers MusicBrainz, RetroAchievements, libretro
  and the built-in keys.
- **architecture.md is behind.** Its last change was 2026-11-01, and it does
  not mention audiobooks, play on another device (`internal/remote`),
  MusicBrainz or `internal/vsync`.
- **The roadmap is too large to read.** It is 688 KB in 2,700 lines, some of
  them 6,000 characters long, and no tool can open it whole. At 1.0, split it:
  `roadmap.md` keeps the status, what is left, open decisions and the backlog,
  and the Releases table and the *what the pass taught* sections move to a
  history file (decision 7).
- **Settle open PR #984** (the uncomposed-video plan). Merge it as written,
  since plans are kept as written, or close it.
- **ADRs 0052 and 0067** still said *proposed* although both shipped (v0.8.46
  and v0.9.28). Corrected in the same change as this plan.

## 4. Order of work

1. **Settings 3.0 steps 3–7, then Profile 2.0.** These ship as v0.10.11 onwards,
   as now.
2. **Scope freeze**, marked by **v0.11.0**. Every release in the v0.11 line is a
   1.0 candidate, carrying fixes and ledger work only (decision 3).
3. **During the freeze, in any order:** the D3 test and the E checks, which are
   code-side and need nobody; the F documents; the B sessions, grouped so
   Georgia is needed once; and the C reproductions, which happen whenever they
   happen.
4. **The last item, §6.**

## 5. Not in 1.0, and said so in the notes

Every item below gets a line in the 1.0 notes, so nobody has to discover it:

- **Live TV.** Parked 2026-08-29: the guide grid, the native-HLS-versus-MSE
  question, and channels nobody is watching that keep running (ADR 0065,
  proposed). Whether it ships visible as a preview or hidden is decision 6.
- **Theater output.** HDR to the display and passthrough are off by default and
  wait for cables.
- **RetroAchievements hardcore.** Built and hidden until RetroAchievements
  approves LANcast. The application can go in from 2027-02-02.
- **Not built:** Chromecast and AirPlay, offline downloads, ebooks (EPUB),
  `.cbr` comics, photo bursts, Last.fm and Trakt scrobbling, bandwidth limits,
  granular sharing, OIDC, theme music, artist images, and a dedicated phone or
  TV client.

## 6. The last work item: the 1.0 acceptance run

The last thing before the tag is not a feature. It is one run of everything a
stranger does, on the release candidate, written down as a checklist so 1.1 can
repeat it:

1. **Build the candidate** from the frozen commit, stamped with the version it
   will ship as.
2. **All three suites pass on that commit** (`go test ./...`, `go build ./...`,
   `npm --prefix web test`).
3. **Clean install** (D1), installer and not bundle.
4. **Update from 0.9.79 and from the latest 0.11.x** (D2), as the installed
   service.
5. **Linux amd64 under systemd** (D4), if Linux is supported.
6. **As the installed service on Aither:** one film down each path (direct
   play, remux, and a transcode with hardware decode), a song, an audiobook, a
   comic, a photo, a retro game, and Watch Together with Utopia. Then read
   `lancastd.log` for `ffmpeg reported errors` and `level=ERROR`.
7. **Then the release itself**, through the ship-release skill:
   - notes written for someone who has never seen LANcast: what it is, the
     four principles, what is in, what is out (§5), and known issues (§3C).
     Not a delta from 0.11;
   - the README status line reads v1.0.0;
   - the roadmap is reset as a 1.x roadmap, split per §3F if that is decided.

**Why this, and not a last feature.** The finding this project keeps making is
that faults are found by using the installed product, not by the test suites.
v0.8.0's hardware decode failed only as the service. v0.9.29's installer failed
only as the installer. v0.9.36 had two faults that a green suite could not see.
A 1.0 tagged off a green CI run repeats the one mistake this project has
already paid for three times. The acceptance run is the release.

## 7. Decisions for Chris

1. **The definition in §1:** 1.0 as a promise (the ledger, install and update,
   the documents), not a feature target.
2. **Scope:** all seven Settings 3.0 steps plus Profile 2.0, then freeze. Or
   freeze sooner, with step 7 moving to 1.1.
3. **v0.11.0 marks the freeze**, and the v0.11 line is the candidates.
4. **Linux in 1.0:** supported (the amd64 archive is run once), or labelled
   experimental.
5. **ffmpeg's privileges:** reduced before 1.0, or documented for 1.0 and
   built in 1.1.
6. **Live TV in 1.0:** visible and labelled as a preview, or hidden.
7. **Split the roadmap** at 1.0.
8. **Proxies, Prometheus and Home Assistant:** labelled "checked with simulated
   clients", unless one of them already runs here.

## 8. Decisions taken (2026-11-08)

Chris took the recommendation on all eight.

1. **1.0 is a promise, not a feature target:** the ledger, install and update,
   and the documents, as §1 defines it.
2. **Scope is all seven Settings 3.0 steps, then Profile Page 2.0, then the
   freeze.** Step 7 stays in.
3. **v0.11.0 marks the freeze.** Every v0.11.x release is a 1.0 candidate,
   carrying fixes and ledger work only.
4. **Linux is supported in 1.0.** The amd64 archive is run once under systemd
   (WSL2 is enough), through a scan and a film played with ffmpeg. arm64 ships
   labelled *built, not run*.
5. **ffmpeg's privileges: cost it, then decide.** Find out what a restricted
   token or a low-privilege account for the child processes costs. A few days
   or less means it is done before 1.0. More than that means security.md says
   so plainly for 1.0, and it is built in 1.1.
6. **Live TV ships visible, labelled as a preview**, in the interface and in
   the notes. The label is the only live TV work, and it does not unpark
   anything else ([the parked scope](roadmap.md#what-is-left) stands).
7. **The roadmap is split at 1.0**, as part of §6 step 7: `roadmap.md` keeps
   the status, what is left, open decisions and the backlog, and the Releases
   table and the *what the pass taught* sections move to a history file.
8. **Proxies, Prometheus and Home Assistant are labelled** "checked with
   simulated clients" in the docs and the notes.

**What these add to the work list:** the costing in 5 (section E), a preview
label on live TV's entry points (section F), and the roadmap split, which
moves into the last item.

## 9. The owner's last notes before the freeze (2026-11-09)

Given after v0.10.11, as the last work before the freeze. Each was looked at in
the code the same day.

1. **A film on a shared library has only play, the scrubber, volume and
   fullscreen.** `PeerControls` is minimal on purpose
   ([ADR 0071](adr/0071-a-shared-library-is-a-standing-grant.md) §5: draw only
   what is true for a friend's film). Subtitles are already fetched across
   servers (`/api/federation/subtitles`) and attached as tracks, but there is
   no control to choose one. Also missing beside the household player: skip
   back and forward, an audio-track choice, and skipping the intro and
   credits, which would need the markers sent across servers (a federation
   route, so the API documents change with it). Which of these are in 1.0 is
   for the owner to say before it is built.
2. **The desktop window should always start maximized.** Today it maximizes
   only if it was maximized when it last closed. Reopening it from the tray
   un-maximizes it as well, because the tray's Show calls `SW_RESTORE`.
   `internal/clientwindow` only.
3. **Tune the music review results.** Read the MusicBrainz match states from
   a copy of the live database
   ([ADR 0115](adr/0115-albums-are-identified-by-musicbrainz.md)), find out
   why albums land in review or unmatched, and adjust the scoring. This also
   clears ledger entry 1 (§3B).
4. **Discuss code hardening.** Section 3E is the starting point. It is a
   written proposal for discussion first, not a change.
5. **The Settings nav needs its own scroll bar.** The nav is
   `position: sticky` with about thirty-five entries and no height limit, so
   on an ordinary screen its last entries sit below the fold until the page
   itself scrolls. It needs a maximum height of the viewport and its own
   scroll.
6. **On Home, a right-click on any title opens More Options.** A tile shows
   a menu only when its shelf passes `actions`, and New this week, Rate what
   you've watched, Watchlist, the pinned shelves, Unwatched and On this day
   pass none. `useItemActions` already gives the library grid and search
   their menus, so Home reuses it.
