# ADR 0029 — Picture-in-picture is our window, not the browser's

Date: 2026-08-10 · Status: accepted

## Context

The player gained a picture-in-picture button in v0.6.7. It calls
`video.requestPictureInPicture()`, which hands the media element to the browser
and lets the browser draw the floating window: its frame, its transport, its
scrubber, its idea of what captions are. Testing it against real media found
four faults, and they turned out to be one fault wearing four coats.

**The clock was wrong.** A transcoded film showed `0:12` and counted up a second
per second on a 1h23m runtime. A progressive fMP4 off a live ffmpeg pipe carries
no duration in its header, so `video.duration` is however much has been produced
so far. `totalDuration` in `PlaybackProvider` already knows this and prefers the
probed runtime — but nothing outside that file could, and Chrome was drawing its
scrubber from the element.

**This one is now fixed, and it matters that it is.** Commit `72619a6` reports
the real timeline through `navigator.mediaSession.setPositionState`, and Chrome
honours it: the PiP scrubber shows the true runtime. That was the fault which
first prompted this ADR, and it has been solved without a rework. It also fixed
Windows' media overlay and the media keys, which had never worked at all.

What remains cannot be reached that way:

**Subtitles do not follow the video.** Turn a track on, pop out, and the cues
keep rendering in the parent tab — over the "Playing in picture-in-picture"
placeholder, where nobody is looking — while the picture they belong to is in
the corner. This is a native `<track kind="subtitles">` with its mode set to
`showing`; where the cues are drawn is the browser's decision once it owns the
window, and it draws them at the element.

**Chrome offers its own captions instead.** The CC button in the PiP window is
Chrome's Live Caption — on-device speech recognition — not our subtitles. So a
file with real, indexed, server-converted subtitle tracks presents a button that
transcribes the audio by guessing at it. It is also the likely cause of a 1–2s
A/V desync observed when it is engaged, since it taps the audio pipeline to do
the recognition.

**Every other control is gone.** Speed, audio track, subtitle selection, queue,
shuffle, repeat, next and previous exist in the bar and vanish on pop-out. The
window that survives has play, seek ±10, and a scrubber.

The common cause is not any of these individually. It is that
`requestPictureInPicture` is an API for handing the element away. Everything the
player knows — the probed runtime, which subtitle track is chosen, what is next
in the queue, that gold means focus and nothing else — stops at the boundary of
a window we do not draw.

There is a second API. `documentPictureInPicture.requestWindow()` (Chromium 116+)
opens an always-on-top window containing *our* DOM. It is the same thing a user
means by "pop out the player": a real floating window, staying on top, with the
player in it.

## Decision

**Pop-out uses Document Picture-in-Picture and renders our own player chrome.
Video PiP stays as the fallback when Document PiP is unavailable.**

- Feature-detect `window.documentPictureInPicture`. Present: pop out into our
  own window. Absent: the current `requestPictureInPicture` path, unchanged, now
  with a correct clock courtesy of MediaSession. Neither available: the button
  hides rather than offering something that cannot work — the rule the control
  bar already follows, and the reason there is no dead button anywhere in it.
- The pop-out window renders the same controls the docked mini-player renders,
  from the same components. Not a second bar that agrees with the first by
  coincidence; the same one. Subtitles, speed, audio track and queue come along
  because they were never anywhere else.
- Our subtitles render in the pop-out because the video element is in the
  pop-out. Chrome's Live Caption button does not appear in a window Chrome is
  not drawing.
- Closing the window returns the element to whichever surface the app is
  showing, exactly as leaving video PiP does today.

**No new dependency.** Document PiP is a platform API; the bundle does not grow
by a byte. This is worth stating next to [ADR 0013](0013-transcode-pipeline.md),
which refused ~300 KB of hls.js and accepted a worse default to hold that line.
Nothing here reopens that argument.

## The hard part, named

The media element must physically move into the other document. This is the one
thing the playback architecture has been explicitly built to avoid.

`PlaybackProvider` keeps a single `<video>` alive above the router and moves it
between the full and docked surfaces *with CSS*, because re-parenting it in
React unmounts it and an unmounted element stops the sound. That trick does not
extend here: a different document cannot be reached with `position: fixed`. The
node has to be appended to the pop-out window's body.

Moving it is legitimate — it is the documented purpose of the API, and the
element keeps playing across the move. The risk is React, not the browser. A
React portal cannot be used, because changing a portal's container unmounts and
remounts its children, which is precisely the thing that stops playback. So the
move is imperative and outside React's reconciliation, which is safe only for as
long as React never needs to touch that node's position among its siblings.

The mitigation is that the element is already rendered exactly once, at the
provider root, unconditionally — the same property that makes the CSS-move
approach work. The failure mode to test for is a re-render pulling the element
back to its original parent mid-playback, which would present as the sound
stopping when something unrelated updates. That is the acceptance test for this
work, and it should be written before the feature is.

Stylesheets do not follow the window either; they must be copied into the new
document, or the pop-out opens unstyled.

### Result: the test was written first, and it found the constraint

`web/src/playback/crossDocumentMove.test.tsx`. The move itself is fine — the
element is adopted rather than copied, keeps its identity, and React goes on
updating its attributes and its `<track>` child while it sits in the other
document. Bringing it home leaves it under React's control.

**One case fails, and it is the shape the provider renders today.** When a
conditional *sibling* mounts immediately before the media element while the
element is away, React calls `container.insertBefore(cover, video)` on a
container the video has left. The DOM throws `NotFoundError`, in the commit
phase, taking down the render pass that did it. `PlaybackProvider` rendered
exactly that: the cover block was a conditional sibling immediately before the
`<video>`.

So the pop-out could not have been built on that markup, and the fix is
structural rather than defensive:

> **The media element must be the only child of a slot whose children React
> never varies.** Everything conditional — the cover, the loading overlay,
> anything added later — is a sibling of the slot, not of the element.

Then the only insert that could use the element as an anchor is one inside the
slot, and nothing else is ever in there. Both shapes are in the test file: the
failing one is kept as a characterisation test so the hazard cannot quietly
return, and the slot shape is proved against the identical toggle that breaks
the other.

This is a constraint on the implementation, not a reason to abandon the
decision. The "keep the fallback" clause below is for the move proving unstable
in ways structure cannot fix; this one it can.

**The provider has since been restructured to the slot shape**, ahead of any
pop-out code, because it is a prerequisite rather than a part of the feature —
and because leaving the hazard in place while the reason for it was fresh would
be the worst of both. The media element is now the only child of
`.playback__slot`, which is `display: contents` so it generates no box and no
other rule has to know it exists. A test asserts against the real provider that
the element is alone in there, so the models above cannot quietly stop
describing their subject.

## Consequences

**Good — the captions problem is solved rather than worked around.** Subtitles
appear where the picture is, the real tracks are selectable, and the
speech-recognition button that competes with them is not present.

**Good — pop-out stops being a downgrade.** Today, popping out costs you every
control except play and seek. It becomes the same player in a smaller window.

**Good — music can pop out too.** Video PiP is video-only, so the button is
hidden for audio. A document window has no such restriction: a floating record
with cover art, transport and queue is a natural fit for the case the
mini-player was built for. Not in scope here, but this decision is what makes it
possible.

**Good — no third-party code, and no API or schema change.** This is entirely
client-side.

**Cost — we own that chrome now.** A second surface that must not drift from the
main bar. Mitigated by sharing components rather than duplicating them, but
"shared" is a claim that decays without a test asserting it.

**Cost — Chromium-only.** Firefox and Safari have no Document PiP, and the
fallback is what they get. Acceptable for a progressive enhancement; it would
not be acceptable for a core path.

**Cost — WebView2 must be recent enough.** [ADR 0023](0023-native-desktop-client.md)
notes WebView2 is evergreen on Windows 11 and updated Windows 10, so 116+ is the
common case — but it is a runtime fact about the user's machine, not a build-time
guarantee, so it is feature-detected like everything else here. An old runtime
gets the fallback, not a broken button.

**Cost — the focus model meets a second document.**
[ADR 0004](0004-keyboard-focus-model.md) assumes one document with one focus
ring. A separate window has its own. Keyboard bindings inside the pop-out, and
what happens to the rail's focus while it is open, need deciding rather than
inheriting.

**Cost — the risk in "the hard part" above is real and is the reason this is an
ADR.** If the imperative move proves unstable under React re-render, the answer
is to abandon Document PiP and keep the fallback, not to restructure playback
around it. Playback surviving navigation is worth more than pop-out fidelity.

## What this does not decide

Whether the docked mini-player is replaced by the pop-out window. They overlap —
both are "keep watching while you do something else" — but one stays inside the
page and one leaves it, and consolidating them is a separate question with its
own design argument. This ADR adds a better pop-out; it does not remove
anything.

## Revisit when

Document PiP reaches Firefox or Safari (the fallback stops being load-bearing),
the imperative element move proves unstable in practice (revert to fallback and
record why), or the mini-player and the pop-out window are consolidated.
