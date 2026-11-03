# ADR 0113 — A visualizer keeps the screen's time, not the page's

Date: 2026-11-02 · Status: **accepted** · Related: [ADR 0111](0111-the-page-leaves-the-screen-over-a-film.md), [ADR 0091](0091-milkdrop-presets.md)

## Context

The music visualizers jittered on the TV. They draw in the page, on
`requestAnimationFrame`, and Chromium ticks that at **one rate for the whole
desktop**: the main monitor's. On a TV at 30 Hz beside a 72 Hz main monitor,
a two-minute PresentMon capture of WebView2's presents
(`viz-milkdrop-20261102-173854.csv`) found:

- the page presenting every **13.9 ms** (72 Hz), and on time: 10 late frames
  in two minutes, GPU busy 3.4 ms at the median;
- the TV taking a new picture every **33.3 ms**, and **5,015 of 8,610 frames
  (58%) never shown**;
- each shown picture standing for **2 or 3** of the page's frames, in a steady
  `3 2 2 3 2 3 2 2 3` pattern. The animation moves by the time between frames,
  so the picture moved 28 ms, then 42 ms, then 28 ms of motion: a 50% swing on
  every picture, for as long as the music played. That is the jitter.

Nothing in the page was slow, and nothing in it could see the problem: rAF's
own timestamps were perfectly even. Only the native side can see a
particular monitor's refresh.

## Decision

**While a visualizer runs in the desktop window on a monitor whose rate is not
rAF's, it draws once per vertical blank of that monitor and moves by exactly
one refresh.**

- `internal/vsync` waits for one monitor's blank (`D3DKMTWaitForVerticalBlankEvent`,
  the call DWM and Chromium use themselves).
- `cmd/lancast/displayclock.go` gives the page two bindings:
  `lancastDisplayRate()` (the window's monitor and its rate) and
  `lancastDisplayTicks(on)`. While on, a thread waits on the window's monitor,
  follows the window to another one within a second, and calls
  `window.__lancastDisplayTick(n, hz)` per blank. `n` counts blanks, so a tick
  the window thread lost shows as a jump and the page moves by both refreshes.
  `hz` 0 means the ticks gave up.
- `web/src/playback/frameDriver.ts` drives all three tiers. A run starts on
  rAF, measures its rate, and asks the window. Only when the two differ by 3%
  or more does it move to the ticks, and it asks again every three seconds.
  It goes back to rAF for good if the ticks give up. It also goes back when a
  tick arrives at rAF's rate, which means the window was dragged onto the main
  monitor. While the window is hidden, the ticks stop.

A tick is delivered as script, so the frame is drawn outside rAF and committed
at Chromium's next frame, up to one main-monitor refresh later. On the
measured desk, that leaves the frame about 20 ms to reach a 33 ms TV refresh.
The motion stays even however late the frame lands within that, because it no
longer depends on when the frame was drawn.

`frameRecord.ts` logs which clock drove each 30 s window (`clock=raf` or
`clock=display@29.97`), so whether this happened is in `lancast-client.log`.

## Consequences

- On the TV, the visualizer does ~40% of the work it did (30 frames a second,
  not 72) and every frame it draws is shown.
- A browser tab has no bindings and keeps rAF. A browser on one screen does
  not have this problem, and a browser on a mixed desk does and can't fix it.
- Rates within 3% (59.94 and 60) count as one clock, and those are left alone.
- The ticks are pushed only while a visualizer asks, and only to a window on
  the wrong clock. A window on the main monitor never receives one.

## What would make this the wrong decision

- Chromium learning to tick rAF per monitor. Then `lancastDisplayRate` agrees
  with rAF everywhere, the ticks are never asked for, and the bindings can go.
- The window thread being too busy to deliver a tick within ~20 ms. The
  recorder's `late=` on a `clock=display` line is that evidence; the
  alternative is to map the blank's time onto `performance.now()` and pace rAF
  from it.

## Rejected

- **Making the TV the main display.** It works, and LANcast never changes the
  primary (the games and theater work both learned that). The app advises it
  for films; it should not need advising for a visualizer.
- **Snapping the animation's time to the screen's refresh while staying on
  rAF.** Without the blank's phase the snapped frames still land two to a
  refresh, then none, as the two clocks drift, which is the same lurch rarer.
- **A blocking binding that returns at the next blank.** Bindings run on the
  window's thread; blocking it for a refresh would stall the whole window.
