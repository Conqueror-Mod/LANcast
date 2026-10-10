# The audio pass — plan

Whispered dialogue and deafening explosions are the standing complaint about
watching anything at home, and no commercial service offers a control for it.
The roadmap entry (Feature backlog → Input and control) called this the
most-felt gap on the page and designed it as a Web Audio graph on the media
element.

**That design was written before [ADR 0067](adr/0067-the-desktop-client-plays-through-libmpv.md),
and it no longer covers the case it was written for.** The desktop client plays
films and episodes through libmpv, and libmpv is not an element. There is no
`createMediaElementSource` for a window mpv draws into. The Web Audio design
still holds for everything that *is* an element, but that is now music and
browser tabs, not the evening film.

So there are two engines and one preference.

| what is playing | where | engine | audio filters run in |
|---|---|---|---|
| film, episode | desktop client | libmpv | mpv's `af` chain (FFmpeg's libavfilter) |
| film, episode | browser tab | `<video>` | Web Audio |
| music | everywhere | `<video>` (PlaybackProvider: `native = !isAudio && …`) | Web Audio |

## What already works

- **The filters are in the shipped DLL.** Our libmpv is built from FFmpeg
  n9.0.2 without `--disable-filters` ([ADR 0069](adr/0069-lancast-builds-its-own-lgpl-libmpv.md)).
  `equalizer`, `acompressor`, `dynaudnorm`, `speechnorm`, `loudnorm`,
  `dialoguenhance`, `aeval` and `pan` all appear by name in
  `C:\Program Files\LANcast\libmpv-2.dll`. That shows they were compiled in. It
  does not show they behave as described below, which is Phase 1's job.
- **A closed command bridge.** `nativePlayer.command` in
  [cmd/lancast/player_windows.go](../cmd/lancast/player_windows.go) takes a
  name and a number, and refuses anything it does not list. That is the shape
  this feature needs.
- **Per-device playback preferences.** [prefs.ts](../web/src/playback/prefs.ts)
  already holds quality, output device, subtitle appearance and offset in
  `localStorage`. A curve is a per-device fact (it is about the room and the
  speakers), so it belongs there and not in the database.

## The rules

**The page never sends a filter string.** `amovie` is in the same DLL, and it
is a lavfi *source* that opens a file by path. A command that accepted a filter
graph from the page would be arbitrary file read by another name, which is
exactly what the closed command set exists to prevent. The page sends numbers,
the Go side clamps them, and one pure function builds the graph from a fixed
vocabulary. This is the same boundary the rest of `command` already draws.

**The filter never changes what the file is.** A 5.1 film that direct-plays
stays 5.1 when a control is touched. On mpv that holds by construction, since
filters run on every channel mpv decoded. On Web Audio it does not, because
`createMediaElementSource` reroutes the element into a graph whose destination
is stereo. Phase 2 has to handle that explicitly.

**No server cost, ever.** Nothing here reaches the transcode pipeline. A
filter in ffmpeg on the server would force a conversion on a file that
direct-plays, and would bake one person's curve into a stream a household
shares. The roadmap entry said this, and it stands.

**A boost lowers everything else.** Raising the centre channel clips on exactly
the scenes that need it, the loud ones. Lowering the other channels by the same
ratio sounds the same once the volume is turned up, and it cannot clip.

**Settings are per content type.** A symphony, a sitcom and a war film do not
want one curve. At minimum `video` and `music` are separate, which also lines
up with the two engines.

## Phase 1: night mode and dialogue boost on mpv

This covers the desktop film, which is the case the complaint is about.

**Two controls, not an equaliser.** An equaliser answers a question most people
never ask. These two answer the one they do.

- **Night mode** (off / on): dynamic range compression. It pulls explosions
  down and dialogue up, so the volume can sit where speech is audible.
  Candidate: `acompressor` with makeup gain. `dynaudnorm` is the alternative,
  and it rides gain over seconds rather than compressing peaks. Which one is
  chosen by measurement, below, not by reading about them.
- **Dialogue boost** (off / low / high):
  - **Surround (≥6 channels):** dialogue lives in the centre channel (FC, index
    2 in FFmpeg's order for 5.1, 5.1(side), 6.1 and 7.1). Lower every other
    channel with `aeval=…:c=same`, which keeps mpv's negotiated layout rather
    than declaring a new one the way `pan` would.
  - **Stereo:** there is no centre to find. `dialoguenhance` derives one (it
    outputs 3.0, which mpv then mixes to the device). Whether it helps enough to
    ship is a listening question. If it does not, stereo gets night mode only,
    and the control says so.
  - **Mono:** dialogue boost is not offered.

**The graph follows what mpv is decoding, not what the probe said.** The audio
track can change mid-film (`aid`), and a stereo commentary track after a 5.1 main
track is common. So `audio-params/channel-count` joins `Observed`, and the Go
side rebuilds `af` whenever the count or a setting changes. The probe's channel
count is a fact about one stream, and this needs the one playing.

**Build order:**

1. `internal/mpv/audiofx.go`: `type AudioFX struct{ Night bool; Dialogue int }`
   and `func Filter(fx AudioFX, channels int) string`. Pure, with table tests
   for every combination × {1, 2, 6, 8} channels, and for the empty chain when
   everything is off. Nothing in the output can come from the caller except a
   clamped integer.
2. `Observed` gains `audio-params/channel-count`. The state carries it, and
   the relay test covers a mid-film change.
3. `command` gains `night` and `dialogue`, clamped like `volume` and `speed`.
   The closed-set test names both, and asserts that an unknown name is still
   refused.
4. `mpvBackend.ts` sends both on open, and again when they change, the way it
   already re-sends volume, mute and speed after a load.
5. Settings → Playback: a **Sound** section with the two controls, per content
   type. The quick toggle in the player chrome is Phase 1b, once the setting
   has been lived with. `design.md`'s rule applies: *on* is not gold.

**Verification, in this order:**

- **Measure the filters offline before wiring them.** Run the exact chain
  `Filter` emits through the ffmpeg already on this machine with `ebur128`, on
  one loud film scene (5.1) and one quiet one. Night mode must cut the loudness
  range (LRA) substantially and leave integrated loudness near where it was.
  Dialogue boost on 5.1 must leave the output 5.1. `ebur128` reports the layout
  and `aeval` does not change it. If a filter does not do what this plan says,
  the plan changes, not the claim.
- **Then in the desktop client**, with the built client swapped in (and the cert
  pin copied: see the memory on locally built clients). Play a 5.1 film, toggle
  both controls, and read `mpv.log` for the filter graph and the output channel
  layout. Then switch to a stereo track mid-film and confirm the graph was
  rebuilt for two channels.
- **Listen.** No test can say whether dialogue is clearer. The stereo
  `dialoguenhance` decision is made by ear, and recorded.

### Measured (2026-09-30, offline)

No test library was on disk, so the chains were run on a **synthetic 5.1
scene** instead. It has quiet pink noise as "dialogue" in FC throughout, and
loud brown-noise "explosions" on the other five channels for 2 s in every 10,
with an independent noise per channel so the effects are uncorrelated. The
graphs are exactly what `AudioFilter` emits, run through ffmpeg 8.1.2 (libmpv
carries 9.0.2; these filters are long-stable, but the difference is noted).

| graph | true peak | layout | FC vs rest, in a burst | burst − quiet |
|---|---|---|---|---|
| none | −1.0 dBTP | 5.1 | −27.2 dB | 34.2 dB |
| night | −0.4 | 5.1 | −27.1 | 25.9 (quiet dialogue +12 dB) |
| dialogue high | −10.1 | 5.1 | −18.1 (+9.1, exactly ×0.35) | 25.2 |
| both | −0.8 | 5.1 | −18.1 | 24.3 |
| stereo, dialogue high | +1.2 (as its input) | 3.0 | FC derived at −7.8 vs sides | — |

What it changed:

- **alimiter normalises by default.** The first night graph measured
  **+1.1 dBTP**, from a limiter set to hold 0.9. alimiter's `level` option
  scales the output back up to full scale after limiting. The graph now sets
  `level=0`. Nobody would have heard the difference, and it would have clipped
  every loud scene.
- **Night mode makes things louder, not quieter.** Its makeup gain lifts quiet
  speech 12 dB and the loud parts only 3.7. So the settings note about turning
  the volume up belongs to dialogue boost alone.
- **Loudness range (LRA) is the wrong instrument for gated noise.** It
  reported night mode *widening* the range (9.0 → 21.6), because the gating
  and percentiles are built for programme material. So range is measured
  directly, as burst-window level minus quiet-window level.
- **Stereo needs uncorrelated effects to mean anything.** With identical noise
  on every channel, `dialoguenhance` read the whole mix as centre and peaked at
  +7.1 dBTP. With independent noise it adds no peak.

None of this says anything sounds better. That is still the listening test.

### Driven in the running app (2026-09-30)

The server was swapped for the test build as the installed service, and the
test client was run beside it with the certificate pin pre-set. The film was
*A Good Day to Die Hard* from the live library (AC-3 5.1(side) plus AAC
stereo, so the track switch could be tested). Everything below is read from
`mpv.log`, in order:

| at | did | mpv |
|---|---|---|
| 0.6 s | open, both off | `[af] (empty)`; 5.1(side) in, remixed to the 7.1 device |
| 59.0 s | dialogue → High | 6-channel `aeval` set, `-> 1`; 5.1(side) through it |
| 72.3 s | night → On | `aeval,acompressor,alimiter`; 5.1(side) end to end |
| 87.4 s | switch to the stereo track | count unknown, so night only. The 6-channel expression never meets 2 channels. |
| 88.3 s | stereo decoded | `dialoguenhance,acompressor,alimiter`, `-> 1`; 3.0 out, remixed to 7.1 |
| 110.0–111.0 s | back to the 5.1 track | night only, then the 6-channel `aeval` again once 6 is reported |
| 116.9 s | night → Off | `aeval` alone |
| 118.8 s | dialogue → Off | `af=""`, `[af] (empty)` |

Not seen: the picture (computer-use cannot capture mpv's video layer) and,
again, the sound. Whether stereo `dialoguenhance` earns its place is still a
decision to make by ear.

### The first listening test tested nothing

The evening's listening ran on the **installed v0.9.44 client**, started from
the Start menu, against the test server. The panel comes from the server, so
both rows appeared. The client predates the `night` and `dialogue` commands,
so it refused every one, and the page swallows a refused command so that
playback never stops over it. `mpv.log` for both films has no `af` set at all.
The reports were "dialogue boost gives some additional clarity on Capote" and
"night mode makes no noticeable difference on Fast & Furious". Both describe
unfiltered audio.

Two things came of it:

- **The rows now need the client to say it can apply them.** The client
  exposes `lancastMpvFeatures()` → `["audiofx"]`. An older client has no such
  binding, and the page reads absence as "none" and shows no rows. This is the
  panel's own rule (absent, not present and inert), broken by a version gap
  that the server-newer-than-client banner exists to describe.
- **Night mode was too weak anyway**, which the next section measures. The
  listening result was wrong for the reason above and right by coincidence.

### Night mode, retuned

Measured on **real programme material** this time: ten minutes of *Shrek
Forever After* (60:00–70:00, AC-3 5.1, action), and the same span of *A
Beautiful Day in the Neighborhood* (DTS 5.1, quiet and dialogue-led), with
`ebur128`. LRA is the right instrument here, where it was not on gated noise.

| graph | action LRA | quiet LRA | level vs off | true peak |
|---|---|---|---|---|
| off | 18.4 LU | 21.8 LU | — | −3.8 / −2.0 dBTP |
| first version (−24 dB, 4:1, 10/250 ms) | 16.9 | — | +11 dB | **+0.4** |
| slow compressor (−35 dB, 6:1, 50/1500 ms) | 11.3 | 11.9 | +7 / +2 | −1.9 / −2.8 |
| `dynaudnorm` | 12.0 | — | +2 | −5.3 |
| `loudnorm` LRA=7 | 11.7 | — | −1 | −3.8 |
| **chosen: −40 dB, 8:1, 50/2000 ms, +14 dB, limit 0.7** | **8.3** | **8.5** | **+5 / −0.6** | **−1.8 / −2.8** |

The first version reacted to bangs and let go between them, so it was a +11 dB
volume change with a 1.5 LU narrowing. The difference that matters, an
explosion and then the next line of dialogue, is a difference between scenes,
so the release went from 250 ms to 2 s. `dynaudnorm` and `loudnorm` measured
nearly as well, and both are ruled out because they look ahead by seconds and
nothing tells mpv to delay the picture to match. On the quiet film the chosen
graph leaves the overall level within 0.6 dB of off, so switching it on is not
a volume jump. Whether it sounds right is, again, the listening test, and this
time it has to run on a client that can apply it.

## Phase 2: the element engine (music and browser tabs)

The roadmap's Web Audio design, plus three traps it did not list.

- **The output-device picker stops working.** PlaybackSettings routes output
  with `element.setSinkId`. Once the element feeds an `AudioContext`, the
  element's sink is bypassed, and the picker keeps "working" while doing
  nothing. The context's own `setSinkId` has to take over, and the picker must
  follow whichever is live. This is the quiet kind of wrong the roadmap keeps a
  section about.
- **`createMediaElementSource` is once per element, for ever.** The graph is
  built on first use and bypassed with gain rather than torn down, or the
  second attempt throws.
- **An `AudioContext` starts suspended** until a user gesture. Resuming it
  belongs in the same gesture that starts playback, or the first track plays
  silent.
- **Surround in a browser tab:** engage only when the source is stereo, or say
  at the switch that it will be mixed to stereo. Never silently.
- **The pop-out** ([ADR 0029](adr/0029-picture-in-picture-is-our-window.md))
  moves the element between documents. The context belongs to the opener. The
  cross-document move test gains an `AudioContext`, as the roadmap entry already
  asked. For music this is moot, since audio does not pop out.

Night mode maps to `DynamicsCompressorNode`. Dialogue boost on stereo is a
peaking `BiquadFilterNode` around the speech band. That is an approximation, and
the settings text should not promise more.

### Built for music (2026-10-05)

Music is the case that shipped first, because the desktop client plays it
through the element (films go to mpv). The graph is `elementAudio.ts`, and the
element routing is `elementEngine.ts`. Its preferences are separate from the
film's (`nightMusic`, `vocalsMusic`).

**Vocals, not a speech-band peak.** In music the voice is almost always mixed
dead centre, so in stereo it is what both sides share. The graph lowers the
side signal (L−R)/2 under the mid (L+R)/2, using the same gains Phase 1 gives
every surround channel but the centre: −6 dB on low, −9 dB on high. As a 2×2
matrix each output is a weighted average of L and R, so it cannot exceed the
input's peak. The quirk of Phase 1's design, that the boost lowers the
surroundings rather than raising the voice, holds here as well.

**Where the controls engage.**
- Night mode engages on mono or stereo only. A `DynamicsCompressorNode` carries
  at most two channels and would fold surround down without saying so.
- Vocals engages on stereo only.
- Both are offered only once the probe has given the track's channel count.

**The traps, as handled.**
- The element is routed once, on the first control engaged, and is never
  unrouted. Off means the graph re-routes itself to a straight wire.
- The context's sink follows the output picker, because the element's own sink
  stops counting once it is routed.
- The context resumes on every play.
- The element is shared with films in a browser tab. So the context's output is
  opened to the device's full channel count, and the graph's ends pass whatever
  arrives. A routed element playing a 5.1 film is not folded to stereo by a
  graph doing nothing.

### Built for films in a browser tab (2026-10-07)

A film in a browser tab uses the same graph and the same numbers as music, and
the film's own preference (`nightVideo`, the one mpv applies on the desktop).
Dialogue boost stays desktop-only: the element is handed stereo, so it has no
centre channel to find.

**What decides is what reaches the element, not what the file carries.**
- A converted soundtrack (`audio_action: encode`) is always stereo, because
  the server mixes it down (`internal/transcode/args.go`). So most films in a
  browser tab can use night mode.
- A directly played or copied soundtrack arrives with the file's own channels.
  On 5.1 the compressor would fold it to stereo, so night mode is not offered,
  and the panel says why rather than hiding the row.

Not yet exercised: the pop-out (ADR 0029) moving a routed element into
another document. jsdom has no Web Audio, so this needs a real browser.

### Phase 2, measured (2026-10-05, offline)

Measured with the exact module the player uses. It was bundled with esbuild and
rendered through Chromium's own `OfflineAudioContext` to 32-bit float WAV, so
any overs survive, then read with ffmpeg's `ebur128`. Three tracks from the test
library:
- a symphony movement: dynamic, with a 20.8 LU range;
- a loud pop master: −9 LUFS, peaking over full scale;
- an acoustic track: already narrow.

**Everything off passes through untouched.** The symphony with every control
off measured exactly as the file did: −18.7 LUFS, 20.8 LU, −0.9 dBTP.

**Night mode: the film's numbers were wrong for music.** Phase 1's −40 dB at 8:1
took the symphony to 6.3 LU, which was fine. It took the pop master from 8.1 LU
to 1.1 LU and 7.8 dB quieter, which is a wall of sound. A sweep:

| setting | symphony (LUFS / LRA) | pop | acoustic |
|---|---|---|---|
| off | −18.7 / 20.8 | −9.0 / 8.1 | −14.9 / 3.2 |
| −40 dB, 8:1 | −19.6 / 6.3 | −16.8 / 1.1 | −17.9 / 1.3 |
| **−30 dB, 4:1** (chosen) | −17.6 / 11.3 | −13.3 / 2.5 | −15.1 / 1.9 |
| −24 dB, 3:1 | −16.7 / 15.3 | −11.6 / 3.6 | −13.9 / 2.1 |

The chosen setting does three things:
- it roughly halves the symphony's range;
- it keeps quiet material within about a decibel of where it was;
- it brings the loud master down 4 dB.

The gap between the loudest and quietest of the three shrinks from 9.7 dB to
4.3 dB. That is what late-night listening wants. The listening test decides
whether pop at 2.5 LU is still too flat.

**The ceiling.** The first ceiling bent towards full scale and measured
+1.4 dBTP. Inter-sample overs ride on top of a waveshaper's output, as they do on
Phase 1's sample-peak limiter. A curve that is linear to 0.5 and never passes
0.7 holds every render at −1.1 dBTP or below.

The ceiling is a waveshaper, not a second compressor, because
`DynamicsCompressorNode` always adds make-up gain. A "limiter" built from one
makes everything louder.

**Vocals did exactly what the matrix says.** The side signal fell 6.1 dB on low
and 9.2 dB on high, against the mid, on every track. Overall loudness fell
0.6–1.2 LU, which is the side content leaving. Peaks only fell: the pop master,
+1.0 dBTP off, measured +0.6 with vocals on.

Neither number says anything sounds better. That is the listening test, in the
desktop client, on the owner's speakers.

### The listening test (v0.9.57, 2026-10-05)

On the owner's speakers, through the desktop client, on three tracks of his
choosing: Woodkid, The 69 Eyes' "Devils", and AJR.

**Night mode: clear, and too loud.** Woodkid was "loud but very clear". Devils
was "quite loud". The measurement explains both. `DynamicsCompressorNode` adds
make-up gain of its own, about +13 dB at −30 dB and 4:1, and the trim after it
was 0. So every track came out evened but between −13 and −18 LUFS, which is
ordinary listening level, not a night one.

The trim is now −5 dB, and every track lands near −20 LUFS:
- the pop master, −9.0 → −18.3;
- the symphony, −18.7 → −22.6;
- the acoustic track, −14.9 → −20.1.

The loudness ranges are unchanged (the trim is a straight gain), and every peak
is at −2.1 dBTP or below.

**Vocals: removed.** Neither level made a discernible difference, and the track
sounded better without it. The numbers had already said why, without anyone
reading them that way. On the pop master the sides were already 9 dB under the
centre, and in a modern mix the bass, drums and lead instruments share that
centre with the voice. Lowering the sides by 6 or 9 dB took out very little and
narrowed the stereo image. The measurement proved the matrix did what it said;
it could not prove that what it said was worth doing.

Lifting a voice in music needs one of two things:
- a presence band on an equaliser (Phase 3);
- real centre extraction in the frequency domain.

Neither is planned on the strength of this. Phase 1's dialogue boost on films is
a different filter, on a different engine, and is not affected.

### Still louder, and why the lab could not see it (v0.9.58)

After the trim, night mode was still louder than off on the owner's machine.
Turning off Sonar's Smart Volume did not change that. Every instrument said
the opposite, offline and in a live `AudioContext`: 9 dB quieter on the loud
master, no distortion, and the player's volume slider honoured. Every one of
those instruments tapped the graph *before* the destination.

The destination was the difference. To keep a browser-tab 5.1 film from being
folded down, the routed output was opened to the device's full channel count.
The owner's default output is Sonar's virtual device, which reports 8 channels
(7.1). So once night mode had been used, every stereo track left as a 7.1
stream, where the element on its own sends stereo. Night mode changed what
reached the mixer as well as the level, and Sonar mixes the two differently.

The output now carries what the source has: stereo for stereo and mono, six
channels for a 5.1 film, capped at the device. Re-applied whenever the source
changes. Whether this was the whole of "louder" is the owner's next listen. It
is the right shape whatever the answer, since a routed element should send the
mixer the same stream an unrouted one would.

**The lesson for this kind of feature:** measure at the point the sound leaves
the page, not only where the graph ends. A tap before the destination cannot
see what the destination does.

### The real cause: the volume slider sat before the compressor (v0.9.59)

On v0.9.59, night mode was still louder than off. That held with Sonar on, and
with Sonar off on the Windows default device. Three fixes in a row had changed
something that was not the cause:
- the trim;
- Smart Volume;
- the output channel count, which was a real fault, but not this one.

That repetition was the signal to stop and look for what every measurement had
held constant. **Every lab run was at full player volume.**

The element's `volume` scales the sound *before* a `MediaElementAudioSourceNode`.
So the slider fed the compressor. At a listening level the compressor saw a
signal far under its −30 dB threshold, compressed almost nothing, and still
applied its automatic make-up gain. Measured live on the shipped graph, on the
same stretch of the loud master:

| player volume | night on vs off |
|---|---|
| 1.0 | −9.0 dB |
| 0.25 | −0.7 dB |
| 0.1 | **+4.1 dB** |

**Fix.** Once the element is routed, it plays at full volume, and the slider is
a gain *after* the graph (`ElementEngine.level`, set through `setElementVolume`).
The compressor always sees the track at its real level. Measured on the real
engine, at the point the sound leaves the page, at volume 0.1: night mode is
**8.9 dB quieter** than off.

**What to keep from this:** a dynamics processor's result depends on its input
level. Every control that scales the input, the volume slider above all, has
to be one of the measured variables, or placed after the processor.

## Phase 3: the equaliser

Bands on both engines (`equalizer` in lavfi, `BiquadFilterNode` in Web Audio),
with a preset per content type. It comes last because it is the control fewest
people use, and because Phases 1 and 2 will have settled the preference model
it sits in.

**Built for music (2026-10-09; Chris chose music only, presets plus five
bands).** Films keep night mode and dialogue boost.
- **Bands** (`elementAudio.ts` `EQ_BANDS`): a low shelf at 60 Hz, peaking at
  230 Hz, 910 Hz and 3.6 kHz (Q 1), and a high shelf at 14 kHz. Each is
  ±12 dB.
- **Placement in the graph:** the bands come before night mode, so night mode
  evens out the shape that was asked for.
- **Clipping:** a pre-cut equal to the largest boost stops a boost clipping,
  because nothing downstream catches a peak while night mode is off.
- **Bypass:** the filters are only made the first time the equaliser is used.
  A flat setting is the straight wire it always was.
- **Presets:** Flat, Bass boost, Treble boost, Vocal presence and Loud at low
  volume. These are starting points to be tuned by ear, not measurements.
  Moving a band makes the setting Custom.
- **Scope:** per device, in `prefs.eq` and `prefs.eqPreset`, and on both music
  paths: the single element, and the gapless deck's shared graph.

## Not in scope

- **Loudness normalisation across a library** (ReplayGain-style). That is a
  scan-time measurement stored per item, which makes it a server feature with a
  schema column. It is worth doing for music and it is a separate plan. Music
  metadata is also being worked on elsewhere at the moment.
- **Watch Together.** The curve is per device, so there is nothing to
  synchronise.
