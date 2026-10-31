# Kaleidescape comparison study

Date: 2026-10-31 · Kaleidescape: private, Mountain View CA, founded 2001.
Current line: Strato K / V / E / M players, Terra Prime servers, its own movie
store. Code compared against `origin/main` at #970.

This is a study, not a plan. Nothing here is decided. Where a difference is a
decision LANcast already made on purpose, it is recorded as such rather than as
a gap.

**What the sources are.** Kaleidescape's own pages, its support articles, the
Home Assistant and openHAB integrations (which document the control protocol
better than any marketing page), trade reviews (TechRadar, Sound & Vision,
What Hi-Fi, CE Pro, Audioholics), the owners' forum kscapeowners.com, and one
long owner write-up. **Reddit could not be read directly**: it refuses both the
fetch tool and the built-in browser. What Reddit opinion is here came through
search snippets (r/Control4, r/hometheater) and is thin. The owners' forum is
the better source anyway, because it is where the people who paid for it argue.

---

## What Kaleidescape actually is

Not a media server in LANcast's sense. It is **a movie store with a hardware
player attached**, and every design choice follows from that.

- **The store.** ~12,000+ films and 2,000+ TV seasons (2021), bought or rented,
  downloaded in full to local storage before playback. Purchases are licences
  tied to an account, re-downloadable, usable in up to five homes. US titles
  often arrive days before disc or digital release; pre-orders download
  automatically.
- **The files.** Their own encodes of studio masters. 4K averages **~80 Mbps**
  against ~15 Mbps for streaming; the new "4K Cinematic" format averages
  **~120 Mbps with 4:4:4 chroma**, exclusive to the Strato K. Audio is the
  disc's lossless track — TrueHD/Atmos, DTS-HD MA/DTS:X — passed through
  untouched.
- **The players.** Strato K ($4,995, 8K, 4K Cinematic), V ($3,995), E ($2,995),
  M ($1,995, HD only). Each has a ~1 TB SSD — **six to ten films** — and deletes
  a played download after 48 hours unless a Terra server is grouped.
- **The servers.** Terra Prime, 8 TB to ~246 TB, SSD or HDD, 2.5 GbE, up to four
  per system, any number of players. Grouping a server is what unlocks the
  covers view, the list view, parental controls and the child remote — the
  standalone player has a reduced interface on purpose.
- **The people.** A "Movie Guide" team in six countries hand-annotates titles:
  synopsis, cover, aspect ratio, where the film starts and ends, where the
  credits begin, intermissions, notable scenes, every song in a musical or
  concert.
- **The dealers.** Sold and installed through integrators. Control4, Crestron,
  Savant, Josh.ai and Lutron are first-class integrations.
- **History that matters.** It began (2003) by copying the owner's DVDs to disk;
  the DVD CCA sued in 2004 and the case ran until 2014. Disc import is gone.
  The company moved from "your discs, on a server" to "our store, on our
  hardware" — which is exactly the line LANcast sits on the other side of.

So Kaleidescape answers "how do I watch a film at disc quality without the
disc", and LANcast answers "how do I watch *my* files". The overlap is the
playback chain and the room around it — and that is where this study looks.

---

## Where LANcast is already doing it right

**You own the files.** Kaleidescape's purchases are licences; owners say so
themselves and compare it to Steam. LANcast plays files on the owner's disks,
and a remux of a UHD disc is the *same* bitstream Kaleidescape is selling —
often higher bitrate than their 80 Mbps average, since UHD discs run to
100+ Mbps. On picture quality per title, a direct-played remux is at parity
with Kaleidescape's standard 4K, not below it. Only 4K Cinematic's 4:4:4 is a
source LANcast cannot have, because no disc carries it.

**No storage ceiling and no proprietary drives.** A $4,000 player that holds
six to ten films, and "movie storage requires Kaleidescape servers (cannot add
your own generic hard drives)", are among the most repeated complaints. The
Terra that removes the ceiling costs "many times the player's price". LANcast
reads any folder on any drive.

**The library shows everything.** A reviewer notes the Kaleidescape interface
shows only what is downloaded to that player, not the whole collection.

**Remote watching, federation, presence, Watch Together.** Kaleidescape has none
of it; a film is watchable in the house it was downloaded to.

**Everything else a home holds.** Music on Strato was dropped; there are no
photos, comics, games, live TV, DLNA or podcasts. Kaleidescape is films and
series and stays that way.

**Price.** The cheapest working system is ~$2,000 plus films at $20–40 each,
and owners argue about studio pricing constantly ($39.99 Warner new releases,
erratic Paramount upgrade fees). LANcast's cost is the drive.

**The interface rules are similar, and ours are written down.** Kaleidescape's
UI is admired for being dense, fast, and free of trends; one owner likes it
precisely for avoiding "heavy white space, rounded corners, and low density".
That is `docs/design.md`'s position. The one place they visibly slipped is the
Strato V rendering its UI at 1080p and upscaling it, which owners notice on a
big screen — a reminder that a theater-room UI has to be drawn at the display's
resolution.

**Parental controls.** Their rating-based hiding and passcode map to LANcast's
per-account rating ceiling (applied in `items.go`) and libraries kept from an
account (ADR 0084). Their child remote — a simplified covers-only UI with
hand-picked titles — has no direct counterpart; see the gap list.

---

## What Kaleidescape does better, and why it's better

The premium is not mostly the files. Reviews and owners point at four things,
and three of them are software.

### 1. The player never lies to the display or the receiver

Kaleidescape outputs each film at its own frame rate and dynamic range and
sends the audio as the original bitstream. The display switches to 23.976 Hz
and HDR; the receiver lights "Atmos". Nothing between the file and the room
re-times, tone maps or decodes anything.

LANcast's native player (`internal/mpv/options.go`) sets none of this:

| Behaviour | Kaleidescape | LANcast desktop (mpv) today |
|---|---|---|
| Refresh rate follows the film | Yes | **No.** Plays 23.976 fps on a 60 Hz desktop, so 3:2 judder on every pan. `clientwindow/displayconfig_windows.go` already binds the display-config API, but only to *name* monitors. |
| Lossless audio to a receiver | Bitstream TrueHD/Atmos, DTS-HD/DTS:X | **Decoded to PCM.** Lossless, but Atmos height objects and DTS:X are lost, since only a bitstream carries them. No `audio-spdif`, no WASAPI exclusive. |
| HDR to an HDR display | Passthrough (HDR10, Dolby Vision) | **Not hinted.** No `target-colorspace-hint`, so HDR depends on what mpv does with Windows' desktop mode, not on the film. |
| Dolby Vision | Yes, player-led on Strato | **No**, and a PC can't fix this in software (see what not to copy). |

This is the single largest gap, and the cheapest to close. It is also the
thing Kodi is known for doing well, which is relevant because "Kodi's
customizability" is half of LANcast's own description.

### 2. Hand-written per-title metadata that the room can act on

The Movie Guide's annotations aren't decoration. Each one is a **cue**:

- **Aspect ratio of the picture**, not the container — so a scope screen's
  masking closes to 2.39, opens to 1.85, and follows an IMAX title that changes
  ratio mid-film. The protocol reports it as a ratio plus top/bottom trims and
  a "conservative" ratio for titles that vary.
- **Where the end credits begin** — lights come up.
- **Intermission** — lights to half.
- **Scenes and songs** — jump to a curated moment or a track in a concert.
  Thousands of films carry them; owners use them mostly to show the system off.

LANcast already has the hardest half of this: **intro and credits detection**
(`internal/marker`, ADRs 0054, 0055 and 0083), validated on held-out samples,
plus chapters (#949). It is missing the picture's aspect ratio, intermission,
and named scenes. LANcast's approach — measure it, store it, let a human lock
a correction (ADR 0008) — fits all three, and needs no tagging staff.

### 3. The theater is a client of the player

Every playback state is published on a control protocol (TCP 10000, also
serial) that integrators build against. From the Home Assistant integration,
the player reports:

- **where the film is**: `content`, `intermission`, `credits`, `disc_menu`;
- **play state**: playing, paused, forward, reverse, with speed;
- **video mode**: resolution, frame rate, EOTF, colour space, bit depth,
  chroma sampling;
- **screen mask**: ratio, top/bottom trims, absolute mask positions,
  CinemaScape mode;
- **what is highlighted in the UI**, with its cover-art URL, rating, synopsis,
  cast — so a touch panel can mirror the screen.

Owners' setups dim the lights on play, lift them on pause, set the masking,
and bring up the room at the credits. Lumagen and madVR video processors are
also widely reported to read the aspect-ratio feed for their own scaling.

LANcast has webhooks (`internal/webhook`: `playback.started`,
`playback.stopped`, `scan.finished`, `backup.taken`) and the live event stream
(ADR 0079). That is the right foundation and about a fifth of the vocabulary.
Pause and resume, entering the credits, and aspect changes are the events a
room actually needs.

### 4. Nothing waits

Films are on local disk before anyone presses play; the UI is drawn at 4K60;
there is no spinner. Owners' complaints are about price, not speed. LANcast
direct-plays local files too, so the constraint is the same: **time to first
frame and seek latency** should be measured as a feature, the way
Kaleidescape's whole design treats them.

### Smaller things worth noting

- **Cover shuffle**: highlighting a film regroups the wall around it by
  director, actor or genre. Kaleidescape's own AI search (kAI, 2025) is
  framed as an extension of it. Reviewers like it, but some turn the
  re-ordering off, so it should be an option.
- **Clicking a credit browses the filmography.** LANcast's cast row
  (`CastRow.tsx`) is the same idea.
- **Curated collections** (AFI Top 100). LANcast's collections (ADR 0017,
  0093) cover the mechanism.
- **Owners' bugs are LANcast's tests.** Kaleidescape owners report subtitles
  turning themselves back on after pause or rewind. Track choice surviving a
  pause or a seek is a cheap assertion to own.

---

## How LANcast could do the premium job without the hardware

In rough order of value per effort. Each item notes the rule in `CLAUDE.md`
it has to respect.

### A. A "theater output" mode in the desktop player (highest value)

One switch, off by default, in Player settings, because each part trades away
something LANcast does today:

1. **Refresh-rate matching.** On play, switch the window's monitor to the
   film's rate (23.976 → 23.976/47.952, 25 → 50, 29.97 → 59.94) with
   `ChangeDisplaySettingsEx`, and restore on stop, on exit and on crash. The
   monitor identity work in `clientwindow` already exists. Then turn on mpv
   `video-sync=display-resample` for the last fraction of a percent.
   *Respect*: the display-targeting lessons from games — never change the
   primary display, only the one the window is on.
2. **Audio bitstream passthrough.** mpv `audio-spdif=ac3,eac3,dts,dts-hd,truehd`
   with `audio-exclusive=yes` on WASAPI. This is what carries Atmos and DTS:X
   to a receiver. *Trade*: in-player volume, night mode, the -5 dB trim, the
   visualizers and anything in `audiofx.go` stop applying, because the audio
   is no longer PCM. The UI has to say so rather than leave dead controls.
3. **HDR passthrough.** `target-colorspace-hint=yes` so a 4K HDR10 film puts
   an HDR-capable display into HDR, instead of being tone mapped
   (ADR 0033 tone maps for clients that need it — this is the opposite path
   and must not disturb it).

None of this touches ffmpeg in the service, so the session-0 rule doesn't
apply, but it **does need eyes and ears on a real TV and receiver**: jsdom
and Go tests can't see a mode switch or an Atmos light.

### B. Measure the picture's aspect ratio

A probe-worker pass that samples frames across the film with ffmpeg
`cropdetect` and stores:

- the dominant **active-picture ratio** (2.39, 1.85, 1.78, 1.33);
- a **timeline** of changes for variable-ratio titles (IMAX sequences);
- a "conservative" ratio, as Kaleidescape does, so masking never cuts the
  picture.

This is the marker detector's shape — sample, decide, store a revision,
let a human lock it — and it is useful on its own: the detail page can say
"2.39:1" truthfully, which a container's 16:9 frame never does.
*Respect*: process execution stays split from parsing (pure decision over
cropdetect output, tested on fixtures); it shells out to ffmpeg, so it's
verified **as the service**, with `lancastd.log` read afterwards; name the
decode method, never `auto`.

### C. Scope-screen presentation (LANcast's CinemaScape)

With B in place, a per-device "my screen is 2.39:1" setting lets the native
player zoom scope films to fill the screen (`video-zoom`/`panscan`),
pillarbox 16:9 inside it, and — what owners on Plex's forum ask for and can't
get — **move subtitles up into the picture** instead of into the black bars
or off the screen. LANcast draws subtitles in the page
(`nativeTracks.ts`, cue variables), so it knows where the picture is and can
place them there.

### D. A theater event vocabulary

Extend webhooks and the event stream with:

- `playback.paused`, `playback.resumed`;
- `playback.location` — `content` → `credits`, from the credits marker that
  already exists; later `intermission`;
- `playback.aspect` — ratio and trims, from B, at start and at each change;
- `playback.video` — resolution, frame rate, HDR, from the probe.

Then **a Home Assistant integration**, as its own repository, is the adapter
to Lutron, masking motors and the rest. It's a client of a documented API, so
it fits "everything interesting is a provider" and "no phone-home": the
server talks only to what the owner pointed it at.
*Respect*: API changes are contract changes — `docs/api.md` and
`docs/openapi.json` in the same commit, with the webhook event list in the
spec.

### E. Intermission, scenes and songs

- **Intermission**: rare enough (roadshow epics) that detection should be
  cheap and conservative — a chapter named "Intermission" or "Entr'acte",
  or a long black stretch with music in the second half of a 3-hour film —
  and a manual marker editor that locks.
- **Scenes**: a user-saved named moment with a start and end, per account.
  Kaleidescape's own scenes can't be edited once saved; LANcast's can be.
  Chapters already give every disc remux a scene list for free.
- **Songs**: for concerts and musicals, chapter titles are usually the song
  names. Surfacing them as a track list on the detail page is most of the
  feature.

### F. Smaller

- **A kids view**: Kaleidescape's child remote is a covers-only view of
  hand-picked titles. LANcast has the rating ceiling; the missing piece is the
  simplified surface.
- **Cover shuffle** around the highlighted title, as an option.
- **Time-to-first-frame** as a logged, tracked number, so "nothing waits" is
  measured rather than hoped for.

---

## What not to copy

- **The store, early release windows, 4K Cinematic, 8K.** These are studio
  deals and Kaleidescape's own encodes. They aren't software, and LANcast plays
  the owner's files.
- **Dolby Vision output.** A Strato is a DV-certified source. A Windows PC
  cannot send DV over HDMI the way a certified player does; mpv can tone map
  DV profile 5/8 to HDR10 or SDR, and that is the honest ceiling. Saying
  otherwise would be the "falls back to software" mistake from `CLAUDE.md`
  again: a claim about behaviour nobody watched.
- **Co-Star** (separate audio and video players to reduce interference) and
  "audiophile-grade electronics". A hardware argument.
- **Licensing as the business model.** It is the most resented thing about
  Kaleidescape among its own owners.
- **A reduced interface for the cheap tier.** Kaleidescape withholds the list
  view and parental controls until you buy a server. LANcast has no tiers.
- **The dealer as the integrator.** Kaleidescape's room integration works
  because an installer programs it. LANcast's version has to be something an
  owner can set up from the Home Assistant UI, or it won't get used.

---

## The one-paragraph version

Kaleidescape's premium is a store, a curator and a player that never touches
the signal. LANcast can't be the store and shouldn't be. It already matches the
picture on any remux it direct-plays, and it beats Kaleidescape on ownership,
storage, reach and price. The gap is the last two metres: the desktop player
doesn't match refresh rate, doesn't bitstream audio, and doesn't hint HDR, and
the server doesn't tell the room what the film is doing beyond start and stop.
Closing those — a theater output mode, a measured aspect ratio, scope-screen
presentation with subtitles in the picture, and a fuller event vocabulary for
Home Assistant — is the Kaleidescape experience without the box.

---

## Sources

- Kaleidescape: [home](https://www.kaleidescape.com/),
  [Strato K](https://www.kaleidescape.com/strato-k-movie-player/),
  [Terra Prime](https://www.kaleidescape.com/terra-prime-movie-server/),
  [vs. streaming](https://www.kaleidescape.com/compare/),
  [4K Cinematic](https://www.kaleidescape.com/4k-cinematic/),
  [what the system does](https://kaleidescape.com/what-can-my-kaleidescape-system-do-for-me),
  [CinemaScape](https://www.kaleidescape.com/article/What-is-CinemaScape-technology),
  [Scenes](https://support.kaleidescape.com/article/What-are-Kaleidescape-Scenes),
  [kAI](https://www.kaleidescape.com/news/kaleidescape-unveils-ai-feature-kai-transforming-movie-store-search-and-discovery-for-customers)
- Control protocol, as documented by integrations:
  [Home Assistant](https://www.home-assistant.io/integrations/kaleidescape),
  [openHAB](https://www.openhab.org/addons/bindings/kaleidescape/)
- Reviews: [TechRadar, Strato V](https://www.techradar.com/televisions/home-theater/kaleidescape-strato-v-movie-player-review),
  [Audioholics, Strato V](https://www.audioholics.com/gadget-reviews/kaleidescape-strato-v),
  [CE Pro, Strato V](https://www.cepro.com/news/kaleidescape-strato-v-review-hits-all-the-right-notes/139517/),
  [What Hi-Fi, Strato C + Terra Prime](https://www.whathifi.com/reviews/kaleidescape-strato-c-and-terra-prime-solid-state),
  [Sound & Vision, Strato S](https://www.soundandvision.com/content/kaleidescape-strato-s-4k-ultra-hd-movie-player-review-page-2),
  [Custom Controls explainer](https://www.customcontrols.co.uk/blog/kaleidescape-explained-hardware-movie-store-and-how-to-choose-your-system/)
- Movie Guide: [Home Cinema Choice](https://homecinemachoice.com/node/119102)
- Owners: [kscapeowners store pricing debate](https://www.kscapeowners.com/XF/threads/store-pricing-feedback-debate.4997/),
  [subtitles after pause](https://www.kscapeowners.com/XF/threads/fix-subtitles-after-pause.8172/),
  [Strato V issues](https://www.kscapeowners.com/XF/threads/strato-v-issues.6853/),
  [Austin Morlan, "The Joy of Kaleidescape"](https://austinmorlan.com/posts/kaleidescape/),
  [Polk forum](https://forum.polkaudio.com/discussion/comment/2334418/),
  r/Control4 "Kaleidoscape worth it" (via search snippet only)
- History: [Wikipedia](https://en.wikipedia.org/wiki/Kaleidescape)
- Subtitles on scope screens: [Plex forum](https://forums.plex.tv/t/vertical-position-of-subtitles/656956)
