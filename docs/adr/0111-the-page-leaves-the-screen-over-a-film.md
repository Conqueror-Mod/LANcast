# ADR 0111 — The page leaves the screen while nothing is over a film

Date: 2026-11-01 · Status: **accepted** · Amends [ADR 0067](0067-the-desktop-client-plays-through-libmpv.md), Option A

## Context

ADR 0067 put mpv's picture in a window of its own and the page in a
transparent overlay window stacked over it, so the React controls draw on the
film. That works, and it has a cost nobody could see until frame-rate
matching (#979) made the TV run at the film's rate.

Anything drawn over a swapchain makes Windows' compositor (DWM) blend the two
before they reach the screen, and DWM blends the whole desktop on one clock.
On a desk with other monitors on, that clock isn't the TV's. PresentMon, which
reports how each frame actually reaches the screen, measured it
(`docs/uncomposed-video-plan.md`):

- Every present was `Composed: Flip`.
- A session lost anywhere from 1 to 281 of 2,878 frames in two minutes. Each
  lost frame showed as its neighbour held for two refreshes, which is the
  hitch people saw.
- mpv's own counters stayed clean throughout, because the loss happens after
  mpv has handed the frame over.

The way out is **independent flip**: Windows sends the swapchain straight to
the screen without composing it. A control program (`cmd/mpvprobe` on the
spike branch) using LANcast's own libmpv and window arrangement found the
conditions, and LANcast then met them. Every present was
`Hardware: Independent Flip`, with **0 frames lost and 2,873 of 2,873 shown for
exactly one refresh**. Three things have to hold together:

1. **The TV is the primary display.** The RTX 3060's driver offers overlay
   planes to the primary display only (`dxdiag`: four planes on the primary,
   none elsewhere). With the TV secondary, every run was composed, mpv's own
   top-level window included.
2. **The picture covers the whole monitor.** Maximized isn't enough: the title
   bar leaves it 34 px short.
3. **Nothing is over the picture.** Not the page, and not the auto-hidden
   taskbar's 2 px edge, which only gets out of the way of a fullscreen window.

Things ruled out along the way, so they aren't tried again: night mode's audio
filter, mpv being embedded rather than top-level, and the GPU time per frame.
Each looked guilty in one comparison and was cleared by repeated runs. Single
runs of the same setup varied from 0 to 281 lost frames, so **one capture
proves nothing**.

## Decision

**While a full-size film has nothing on screen over it, the page's overlay
window is hidden.** When the controls fade out, the overlay is drawing nothing
anyway; its only effect was to force composition.

- **The page decides and the window follows**, as `lancastMpvLayout` already
  works. The page computes whether anything is showing (`overlayIdle` in
  `Player.tsx`) and tells the window through `lancastMpvOverlayIdle`. The
  answer is no while any of these is up:
  - the controls;
  - subtitles;
  - a skip or up-next card, or the intro-skipped notice;
  - the stats panel;
  - the still-watching prompt;
  - a paused or loading film.

  The rule lives in one place and is a jsdom test.
- **The window only acts on a full-size film with no game under it**
  (`webview2/overlay_idle.go`). A docked film or picture-in-picture never hides
  the page, and a layout change brings a hidden overlay back at once.
- **Input while hidden:**
  - The main window holds activation, so a key reaches it. The key wakes the
    page and is then given to it, so Space pauses rather than only waking.
  - A mouse move wakes the page only when the pointer moves **over the
    picture**, and by at least 24 physical pixels from where it rested
    (`pointerWakes`). Moving the mouse on another screen, to type into
    another window, leaves the film alone. The first build woke on any
    movement anywhere, and every reach for another window brought the
    controls up over the film. The distance is there because every wake has
    a cost (below), and a bump of the desk should not pay it.
  - Media keys go through `winkeys` as before.
- **mpv hides the cursor** over the picture while the page is away, since the
  page's `cursor: none` went with it.
- **A film opens fullscreen while frame-rate matching is on** (Theater output,
  *Open films fullscreen*, on by default under matching). The first film
  after a restart opened maximized, as it always had, and two minutes of it
  were every frame composed: the title bar and the taskbar's edge were over
  the picture. The window binding `lancastEnterFullscreen` only ever enters,
  so a film opening in a fullscreen window leaves it so, and a client without
  it changes nothing. Once per film: leaving fullscreen mid-film holds until
  the next one.

**LANcast does not change the primary display.** Which screen is primary
moves the taskbar, the Start menu and where new windows open, and it's the
person's desktop, not the player's to rearrange. The player says it instead:
the Theater output settings, and the `display timing` log line, name the
primary display as the thing to change.

## Consequences

- **Subtitled films stay composed.** Subtitles are drawn by the page
  (`nativeTracks.ts`), so the overlay is needed while they show. Having mpv
  draw them instead (libass is already in the build) would mean two renderers
  that must match in style. That's its own decision, if subtitled films need
  it.
- **A fixed ~0.1 s more latency.** In independent flip, frames queue about
  124 ms deep against about 20 ms composed. mpv times the picture to the
  audio, so sound and picture stay together. It shows only as a slightly
  slower response to pause or seek.
- **Every wake costs two stutters.** Bringing the overlay back switches
  Windows from independent flip to composed, and hiding it again switches
  back. Measured over three wake-and-fade cycles: each switch held two or
  three frames for two to three refreshes, and the fade back dropped one, so
  a wake costs about a tenth of a second of hitch on the way in and again on
  the way out. While the controls are up, the film is composed again, with
  the old 1.4/0.6-refresh judder in bursts. None of this can be designed
  away while the page is a separate window. What LANcast controls is how
  often it happens: only a deliberate move or a key wakes the page, and a
  key that does is not wasted, because it also acts.
- **Nothing changes for anyone whose TV isn't primary**, or who watches in a
  window. The overlay still hides, but Windows composes regardless, as before.

## What would make this the wrong decision

- **A driver that gives overlay planes to every display.** Then the primary
  display guidance is moot, and the hiding still helps.
- **A WebView2 that can be uncomposed over video**, through DirectComposition
  overlay support. Then the page could stay up, subtitles included.

## Rejected

- **Make the TV primary for the length of a film.** It's possible
  (`ChangeDisplaySettingsEx` with `CDS_SET_PRIMARY`), but it moves the person's
  taskbar and windows every time a film starts and stops, and a crash
  mid-film would leave the desktop rearranged. Display changes made for the
  person have already gone wrong once in this room (2026-10-31).
- **Leave the overlay up and resample to the compositor's clock.** Measured
  in #981: worse, with 70–200 late frames a minute.
