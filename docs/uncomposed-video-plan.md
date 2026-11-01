# Uncomposed video plan

Date: 2026-11-01 · Follows [the theater output plan](theater-output-plan.md),
step 1 (#979, #981, shipped in v0.10.0). Code read at `main` after #983.

## The problem, as measured

With frame-rate matching on, the TV runs at the film's rate (23.976 Hz). But on
a desktop with other monitors on, the film's frames don't reach it on the TV's
clock:

- mpv, pacing to the display (display-resample), measured its own presents at
  **71.5–71.9 Hz** with the desk monitor C27F398 on, and **59.7 Hz** with only
  display 2 beside the TV. That's never the TV's 23.976. Resampling to that
  produced 70–200 late or mistimed frames a minute (`playback timing`, #981).
- With audio as the clock (what v0.10.0 does on a busy desk), mpv's counters
  are clean (3 dropped frames in about 40 minutes), yet a slight hitch is
  still visible now and then and recovers by itself. mpv hands frames over on
  time. Windows then shows them on the other monitor's beat, and 71.91 Hz is
  near 3 × 23.976 (71.928) but not equal, so the two slip past each other
  about once a minute.
- With the TV as the only display, the same film played smoothly.

**Why:** the page's controls are drawn in a transparent overlay window stacked
over the video window (`internal/webview2/overlay.go`, ADR 0067). Anything
drawn over a swapchain makes Windows' compositor (DWM) blend the two before
scan-out, and DWM composes the whole desktop on one clock. So the film is
paced by whichever monitor DWM is following, not by the TV.

No timing setting inside mpv can fix this, because mpv never sees it. The
picture has to reach the TV **without DWM composing it**.

## What Windows offers, to be confirmed by measurement

Each line below is a claim about Windows to be verified with the instrument in
Phase 0, not assumed. The ffmpeg section of `CLAUDE.md` applies: a comment
asserting behaviour nobody watched has cost a release before.

| Path | When Windows uses it | Overlay allowed? |
|---|---|---|
| **Composed: Flip** (today) | Anything over the swapchain, or not fullscreen | yes |
| **Independent flip** | A flip-model swapchain covering the whole monitor, nothing over it | no |
| **Multiplane overlay (MPO)** | The GPU puts the video on its own hardware plane, and DWM composes the UI on another | yes, if the hardware and swapchain format qualify |

Independent flip and MPO are both shown by **PresentMon**, Intel's
open-source (MIT) frame-timing tool, as the `PresentMode` of each present. It
also records `MsBetweenDisplayChange`: how long each frame actually stayed on
the TV. That's the hitch itself, measured where mpv's counters can't see it
(a clean 23.976 film reads a steady 41.7 ms; a hitch reads a 0 or an 83 ms).

## Phases

### 0. The instrument

Run PresentMon for two minutes of a film on the current build, and keep the
CSV as the baseline. Expected: `Composed: Flip`, with display-change intervals
showing a periodic irregularity on the busy desk and none with the TV alone.

- PresentMon is a download from Intel's GitHub releases. **It needs Chris's
  OK before it's fetched** (file, source, size), and it's run from its own
  folder, never from the repo.
- It reads ETW events, which needs an elevated prompt or membership of
  *Performance Log Users*. That's Chris's machine-level decision. Elevated, a
  run is one command he pastes.
- No measuring, no spike: every phase below is judged by this CSV.

### 1. Spike: hide the overlay while the controls are hidden

The player already hides its controls after 2.5 s without input
(`web/src/screens/Player.tsx`, `idleTimer`). In that state the overlay draws
nothing, and its only effect is to force composition.

Spike: in the full layout, with the window covering the TV, hide the overlay
window (`SW_HIDE`) while the controls are hidden, and show it again on the
first mouse move or key. Then measure.

- **Win:** `PresentMode` becomes an independent flip while the controls are
  hidden, and display-change intervals are steady at 41.7 ms on the busy desk.
- **Unknown to settle here:** mpv's `--wid` mode draws into a child window it
  creates inside LANcast's video window. Whether Windows grants independent
  flip to a child window's swapchain is exactly what this measurement answers.
- **Cost to measure:** the switch between composed and independent flip may
  show one glitched frame each way. If it does, it happens when someone has
  just touched the mouse, which is the best moment for it.

### 2. Spike, only if 1 fails on the child window

Have mpv's swapchain belong to a window Windows treats as top-level. mpv's
`--wid` handling, and whether a borderless top-level video window can host it
instead, are the questions. libmpv's render API doesn't offer D3D11, so
rendering into LANcast's own swapchain isn't available.

### 3. Spike, independent of 1–2: MPO

If the RTX 3060 and Windows 11 put mpv's swapchain on an overlay plane,
`PresentMode` reads `Hardware Composed: Independent Flip` *with the overlay
still visible*. That would keep the controls and subtitles on screen and still
leave DWM's clock. Today's swapchain is `R8G8B8A8_UNORM` (mpv.log), and MPO
often wants a YUV or 10-bit format, so the spike tries mpv's
`d3d11-output-format` options and measures.

### 4. Decide, then an ADR

Whichever path wins is a change to how ADR 0067 layers the page over the
picture, so it gets an ADR before the code.

## Design questions phase 1 raises (answered before building, not after)

- **Subtitles are drawn by the page** (`web/src/playback/nativeTracks.ts`), in
  the overlay. Hiding the overlay hides them. The options:
  - keep composing while subtitles show (simple, and the hitch remains for
    subtitled films);
  - let mpv draw them on the uncomposed path (libass is already in the build,
    unused, per `third_party/libmpv/PROVENANCE.md`), at the cost of two
    renderers that must match in style.
  - MPO (phase 3) avoids the question entirely.
- **What counts as "nothing to show".** The page knows: controls hidden, no
  subtitles, no Skip intro, no Up next card, no toast, no menu. It decides
  `overlayNeeded` and tells the window through a binding, so the rule lives in
  one place and is a jsdom test.
- **Input while the overlay is hidden.** The page holds keyboard focus inside
  the overlay. With it hidden, the video window (or the main window) gets the
  input. A mouse move shows the overlay; a key shows it and is then handed to
  the page. Media keys already go through `winkeys` and keep working.
- **Windowed playback** is never uncomposed. Independent flip needs the whole
  monitor, so this only applies when LANcast is fullscreen on the TV, which is
  how a film is watched there.

## Rules this has to respect

- **No third-party player library** (CLAUDE.md, ADR 0013/0050). PresentMon is a
  measuring tool run by hand, never shipped or called by LANcast.
- **Verified on the theater PC as the user runs it**, not from a shell. The
  same lesson as the session-0 rule: a compositor behaves differently for a
  window in a different state, and only the real one counts.
- **Gold is earned:** nothing here changes the look of the controls, only when
  their window exists.
- **One owner for layout:** the page decides when the overlay is needed, and
  the window only follows, as `lancastMpvLayout` already works.

## Meanwhile

On the busy desk, v0.10.0 is close: clean frames from mpv, and a slight
periodic hitch. For a film where it matters, switching the desk monitors off
at their power buttons may remove it. A powered-off monitor usually drops out
of Windows' layout, which isn't the same as rearranging displays in Settings
(which went badly once, 2026-10-31). LANcast puts the TV back on its own if the
layout changes mid-film (#981).
