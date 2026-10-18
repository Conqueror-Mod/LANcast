# Now Playing and visualizers for music — plan

**Status: step 1 built (#896, 2026-10-18).** Steps 2 and 3 (the visualizers)
are not started. Where step 1 departed from the proposal below, the text has
been brought into line and says so; the rest is the original proposal.

A proper Now Playing view for music, and a few visualizers in the spirit of
Winamp's: the classic spectrum and scope first, then the feedback-trail look
that most people remember Winamp for. Client-only work: no API change, no schema
change, and no new third-party library.

## What already exists

The full player, `/watch/:id`, already plays a track, and the mini player
expands to it. For a track it shows:

- the cover as the provider's surface (`.playback__cover` in
  `PlaybackProvider.tsx`): a blurred wash of the art behind the art itself, at
  `min(46vh, 46vw)`;
- the title and `artist — album` in the top bar, at the size a film's title gets;
- the full transport, plus shuffle, repeat, add to playlist, night mode and
  pop-out;
- the queue (`QueuePanel`) and lyrics (`LyricsPanel`), each as a popover that
  covers the screen when opened.

So Now Playing is **this screen's audio layout, redesigned**. It is not a new
route. The route, the mini-player hand-off, the one media element moved with
CSS, and every control stay as they are.

What is wrong with the screen today is that it is a film player with a cover in
it: the track is identified in small type in a corner, what comes next is
hidden behind a button, and lyrics cover the art to be read.

## Where the audio comes from

Music plays on the deck (`playback/musicDeck.ts`): two `<audio>` elements that
are not in the DOM, or on the shared element when the deck does not apply. That
is true in the desktop window as well, because mpv plays video only. Both paths
can already feed one `AudioContext` (`applyDeckFX` / `applyElementFX` in
`playback/elementEngine.ts`). A visualizer needs an `AnalyserNode` on that
graph and nothing else.

The cost is the engine's own rule. **An element that has never had an effect
turned on is left un-routed**, so it plays exactly as the browser would. A
visualizer has to route it. The traps in routing are already handled,
because night mode found them: the slider becomes the `level` gain after the
graph, and the output follows the source's channel count rather than the
device's. With every effect off, the graph is a straight wire. But a context is
never torn down, so once the visualizer has been shown, that session's music
plays through Web Audio until the page reloads. That is acceptable, and it
should be said in the code where it happens.

Two placements matter:

- **Before `level`.** The bars should react to the track, not to where the
  volume slider is.
- **After the deck's crossfade (`graph.output`).** Then one analyser follows
  both deck elements through a gapless join or a crossfade, with no handover of
  its own.

Per the Web Audio spec, an analyser's output may be left unconnected. Step 2
proves it reads data that way in WebView2 rather than assuming it.

## The layout

### Wide screens

```
┌──────────────────────────────────────────────────────────────┐
│ ←                                                      ⋯     │
│                                                              │
│   ┌────────────────────┐      NOW PLAYING                    │
│   │                    │      Title of the track             │
│   │   stage:           │      Artist — Album · 1994          │
│   │   cover or         │                                     │
│   │   visualizer       │      UP NEXT  │  LYRICS             │
│   │                    │      ─────────────────────────────  │
│   │                    │      2  Next track          4:12    │
│   └────────────────────┘      3  The one after       3:58    │
│                               …                              │
│                                                              │
│ ─────────────────────●──────────────────────────  1:42 / 4:30 │
│   ⇄  ⏮  ❚❚  ⏭  ↻   +   night   pop-out           volume     │
└──────────────────────────────────────────────────────────────┘
```

- **The stage** is the square the cover sits in now. It shows the cover or a
  visualizer, chosen by a small switcher under it. The choice is remembered per
  device, alongside the other "This device" preferences.
- **Identity in content type, not chrome type.** The title is large and in
  sentence case. `artist — album · year` sits under it. The wide-tracked
  `NOW PLAYING` label is the only structural text (design.md, Typography). The
  top bar loses the title, because it is now on the page.
- **The queue is on the page, not in a popover.** It is the queue panel's
  content laid out in a column, with the same click-to-play, and the playing row
  marked the way every "where you are" is marked. It follows the record: when
  the track changes, the playing row goes to the top. **Lyrics is the column's
  other tab.** Synced lyrics already follow `displayTime`, so they move into the
  column unchanged. On a track both popover buttons leave the bar; a film keeps
  its queue popover.
- **The tab is called Queue, not Up next.** This proposal said Up next, but
  inside the queue "Up next" already names the lane of tracks queued by hand,
  and one name for two lists is confusing. Changed when built.
- **The field is the cover, tinted into the field.** The blurred wash stays, but
  screened with `--nebula-violet` and `--nebula-blue` and vignetted, as
  design.md requires of any full-bleed artwork. Today's wash is untinted, so the
  screen reads as a photograph with an app around it.
- **The transport is unchanged.** It is the same bar with the same buttons in
  the same order. Nothing about this plan needs a new control there except the
  stage switcher, which sits with the stage.
- **The chrome still never hides on audio.** Player.tsx already skips the idle
  fade for a track, because there is nothing to get out of the way of. Full
  screen visualizer (below) is the one exception.

### Full-screen visualizer

From the stage switcher, or `v`. The visualizer fills the screen, and the chrome
fades after 2.5 s idle exactly as it does over a film. It comes back on mouse
movement or any key. This is the party mode and the ten-foot mode: on a TV it is
the reason to open the screen at all. Escape returns to Now Playing, not out of
the player.

### Phone (≤ 600px)

Stacked: the stage at the top, the column under it, and the control bar below,
with the phone player's "⋯" menu kept for everything past the essentials. As
built, the column stays a column rather than becoming a sheet: the same tabs,
with about two and a half queue rows visible at 375×812. Look at it at 375px
before calling it done: jsdom will pass a column that sits off the screen. The
first pass ran 45px under the control bar, which measures 153px there.

### Keyboard, pad, remote

The stage switcher and both column tabs are focusables in the spatial model, so
a pad reaches them with the arrows. `v` toggles the full-screen visualizer.
Inside it, `←`/`→` stay seek, as everywhere in the player, and `,`/`.` move to
the previous or next preset. Not `[`/`]`: that pair is the subtitles binding in
`lib/keys.ts`, and a key that means two things depending on what is playing is
the gold rule's problem in another form. Both new bindings go in
`DEFAULT_BINDINGS`, so they are listed under `?` and rebindable like every
other.

## The visualizers

### Colour

**No gold, anywhere in a visualizer.** Gold means where you are; a spectrum
drawn in it would put full-strength gold on screen at all times and kill the
focus signal on the one screen a remote is pointed at.

The palette comes from the cover: downsample the art to a few pixels on a
canvas and take two or three dominant colours, nudged off any hue near gold.
Without a cover, the nebula colours. This is presentation, not truth, so it
stays on the client and needs nothing from the server.

### Photosensitivity

Strobing is the obvious failure of this genre, and the rule is WCAG's: **no more
than three full-field flashes per second.** In practice:

- beat-driven changes to brightness or palette ease over at least 300 ms, and
  never flip the whole field;
- a **calm** setting halves the motion and turns off beat-driven palette shifts;
- `prefers-reduced-motion` means the stage defaults to the cover. A visualizer
  someone picks on purpose is still shown, in calm.

### Tier 1 — classic

- **Spectrum:** log-spaced bars from the FFT, with falling peak caps.
- **Scope:** the waveform as one line.

Canvas 2D. The maths (bin grouping on a log scale, attack and decay smoothing,
peak-cap fall) are pure functions over arrays, tested the way
`probe/decide_test.go` tests decisions: one named case per behaviour, with no
audio and no canvas.

### Tier 2 — feedback trails

The look most people remember as "Winamp" is AVS and MilkDrop's core trick:
**each frame redraws the previous frame slightly transformed, then draws the
new waveform on top.** Zoom a little and the trails rush outward; rotate and
they spiral; blur and fade and they smoke away.

- WebGL2, two framebuffers swapped each frame, with one shader for the transform
  pass and one for the waveform.
- **A preset is a plain object of numbers:** zoom, rotation, fade, blur, how much
  the bass drives the zoom, palette mapping. There is no code in a preset, so
  nothing is evaluated.
- **Beat detection** is a pure function: the energy of the low bins against a
  rolling average. It is tested on synthetic arrays.
- Four to six presets to start, named plainly ("Tunnel", "Spiral", "Smoke"…).
- Without WebGL2, tier 2 is not offered, and tier 1 still is.

### Rendering rules

- One `requestAnimationFrame` loop, running only while the stage or the full
  screen is showing a visualizer. It stops on the mini player, in a hidden tab,
  and while paused. When paused, the last frame holds rather than going black.
- The canvas is sized to its box times `devicePixelRatio`, capped so a 4K TV
  does not render 4K feedback buffers for no visible gain.
- **The canvas is a sibling of `.playback__cover`, never inside or beside
  `.playback__slot`.** The slot comment in `PlaybackProvider.tsx` says why: a
  conditional sibling of the media element breaks the pop-out's cross-document
  move. `crossDocumentMove.test.tsx` already holds that shape, and a canvas is
  exactly the kind of "whatever is added later" it warns about.

## Steps

Each step is its own PR, and each ships something usable on its own.

1. **Now Playing layout. Built (#896).** The stage, the title block, Queue and
   Lyrics in the column, the tinted wash, and the phone layout. No audio change,
   and the stage shows only the cover. Checked in a browser tab against the
   installed server at 1440×900 and 375×812. Still owed: a look in the desktop
   window once a release carries it, and synced lyrics on a real track (the
   album checked had no `.lrc`).
2. **The tap and the classic visualizers.** The analyser on the engine (a way to
   build the engine with every effect off, so the visualizer does not depend on
   `fxActive`), the stage switcher, spectrum and scope, calm, and the
   reduced-motion default. Prove it on both paths, "music on deck" and "music on
   element": bars move, the level is unchanged with every effect off, and a
   crossfade does not reset the analyser.
3. **Feedback presets and full screen.** The WebGL2 pipeline, presets, beat
   detection, full-screen mode with idle chrome, and preset keys. Check it on
   the TV with a pad, since that is where it will mostly be used.

## Testing

jsdom has no layout, no canvas, no WebGL and no Web Audio, so the suite proves
wiring and behaviour, not the picture:

- **Pure:** binning, smoothing, peak fall, beat detection, palette extraction
  from a pixel array, and the gold-avoidance nudge.
- **Wiring:** with the fake context the deck tests already use, check that
  showing a visualizer builds the engine with every effect off, that the
  analyser hangs off the graph's output before `level`, and that the loop stops
  when the surface leaves `full`.
- **Looking at it:** the layout at desktop width and at 375px, and the
  visualizer in the desktop window over a real album with a gapless join in it.

## Not in this plan

- **MilkDrop proper (butterchurn).** It is authentic, but it is a third-party
  library, and its presets compile to JS run with `new Function`, which would
  rule out a strict `script-src` later. Its preset licences vary. If tier 2
  leaves people wanting it, that is an ADR of its own, and it would be vendored
  the way hls.js is: pinned, off by default, in its own chunk.
- **A visualizer in the pop-out window.** It is possible, because the pop-out is
  a portal with the same React context, but it is a second render loop in a
  second document, and the first one should exist first.
- **Visualizers as plugins.** A visualizer is a natural provider: frequency data
  in, pixels out, no other capability. It fits the wazero sandbox. That is for
  after there are built-in ones to define the contract by.
- **Films.** None of this applies to a film.

## Open questions

- Does the visualizer **replace** the cover in the stage, or play **behind** it
  as the field? This plan says replace, with full screen as the "field"
  version, because a visualizer behind a sharp cover fights it.
- ~~Is the queue always on the page on a wide screen?~~ Settled by step 1:
  always, because on a record what comes next is half of what the screen is
  for.
