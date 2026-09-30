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

## Phase 3: the equaliser

Bands on both engines (`equalizer` in lavfi, `BiquadFilterNode` in Web Audio),
with a preset per content type. It comes last because it is the control fewest
people use, and because Phases 1 and 2 will have settled the preference model
it sits in.

## Not in scope

- **Loudness normalisation across a library** (ReplayGain-style). That is a
  scan-time measurement stored per item, which makes it a server feature with a
  schema column. It is worth doing for music and it is a separate plan. Music
  metadata is also being worked on elsewhere at the moment.
- **Watch Together.** The curve is per device, so there is nothing to
  synchronise.
