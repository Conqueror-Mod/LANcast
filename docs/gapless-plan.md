# Gapless and crossfade for music — plan

**Status: proposed (2026-10-09).** The music player's last open item from the
Lyrics entry in the roadmap backlog. This is the work breakdown; nothing here is
built except the measurement in step 0.

## What was measured first (#811)

A meter on the music element writes the silence between two tracks to the
desktop log, split at the next source's `loadstart`. Chris played five
consecutive tracks of a live MP3 album (*Live: Beside You in Time*, tracks
2→6) on 2026-10-09:

| Transition | Total | Our work (ended → src set) | Browser load (src → playing) |
|---|---|---|---|
| 2→3 | 88 ms | 30 ms | 59 ms |
| 3→4 | 69 ms | 29 ms | 40 ms |
| 4→5 | 86 ms | 45 ms | 41 ms |
| 5→6 | 88 ms | 34 ms | 54 ms |

He heard "small gaps, a fraction of a second each". The measured 70–90 ms is
the player's share. MP3 adds its own: encoders pad each file with silence at
both ends (roughly 25 ms of priming at the start and up to about 26 ms of padding
at the end, at 44.1 kHz). So the audible gap is about 100–150 ms.

**Conclusions:**
- **Making the switch faster cannot remove the gap.** Even a 0 ms switch leaves
  the padding inside the files.
- **Our own work is only about half of what the player adds.** Fetching the next
  item early would save about 35 ms and leave the load and the padding.
- **What removes it is having the next track already loaded and started on time.**
  It has to start a fraction early, overlapping the outgoing track by about the
  padding. That mechanism is also crossfade, with a longer overlap.

## Design: a music deck behind the existing backend seam

The player already plays films through an abstraction: `MediaBackend`
(`playback/backend.ts`) is the element's contract, and native film playback
satisfies it with libmpv (`mpvBackend`). Music would get a third backend:
**`deckBackend`**, two `<audio>` elements behind one `MediaBackend` face.

- **Current track:** it plays on the *active* element, and the deck reports that
  element's `currentTime`, `duration` and `paused` as its own.
- **Preloading:** once the active track is within about 20 s of its end and the
  queue knows what comes next (`nextItemID`, which already exists), the deck
  loads that track's direct URL into the *standby* element with
  `preload="auto"`.
- **Handover:** at `duration − overlap`, the standby element starts and the
  active one fades out over the overlap. The overlap is about 60 ms for gapless,
  or the crossfade length. The deck then swaps roles and fires the events the
  provider expects for a new source (`loadstart`, `loadedmetadata`, `playing`),
  so the provider's existing advance path runs, but without waiting.
- **Timing:** the handover is scheduled from `timeupdate` plus a `setTimeout`
  for the remaining milliseconds, because `timeupdate` alone fires only about
  every 250 ms.

### Scope rules

- **Music only, and direct play only.** A track the server must convert has no
  file to preload, so it falls back to today's path, gap and all. Films are
  untouched.
- **The queue stays the queue.** The deck preloads whatever `nextItemID` says.
  Shuffle, repeat, the hand-queued lane and Auto play all keep their meaning.
  Repeat one never preloads, and nothing preloads with Auto play off.
- **Anything that changes the plan cancels the preload:** a skip, a seek near the
  end, a queue edit, or a pause in the last few seconds.

### Things the deck must carry over

- **Night mode and the output device.** Today `applyElementFX` builds one Web
  Audio graph on the one element. Both deck elements need a source node, and
  `createMediaElementSource` works once per element (memory: music audio pass),
  so the deck creates both at once and keeps them. The graph after the sources
  stays one graph, so night mode and `setSinkId` behave as today.
- **Volume is the deck's, not an element's.** The fades are applied per element
  on top of it.
- **The mini-player, picture-in-picture moves, Media Session and the lyrics
  panel** all read through the backend face and should not notice. Proving that
  is most of the test work.

## Settings

Per device, beside night mode in the playback panel:
- **Gapless:** on by default. Its only visible effect is the absence of a gap.
- **Crossfade:** Off (default), 2 s, 5 s, 8 s or 12 s. **Decided (Chris,
  2026-10-09): crossfade never applies between consecutive tracks of the same
  album.** Those always play gapless, because a fade would cut into a live or
  continuous recording. It applies when the album changes, as in a shuffle, a
  playlist or a mixed queue.

## Steps

0. **Measure.** #811, done.
1. **The deck** behind `MediaBackend`, gapless only. Its tests use stub
   elements in jsdom: preload timing, handover, cancelling, and events in the
   provider's expected order. Then the provider choosing the deck for direct
   music.
2. **The Web Audio graph on two elements:** night mode and output device, with
   an offline render proving a handover leaves no dropout and no doubled level.
3. **Crossfade:** the setting and the fade curve (equal-power).
4. **Listening test** in the installed client, on the same live album and on a
   studio album, read against the gap meter, which should then report about
   0 ms.

## Not in this plan

- **Sample-exact gapless** (decoding to Web Audio buffers): a 20-minute live
  track decodes to about 460 MB of PCM. Element overlap gets within a few
  milliseconds, which nobody hears.
- **Trimming the MP3 padding from file headers** (LAME and iTunSMPB): the
  overlap hides it without parsing anything.
- **Music in the desktop's native player:** music stays on the element, as now.
