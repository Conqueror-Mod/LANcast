# ADR 0043 — Media tools are fetched on request, not bundled

Date: 2026-08-18 · Status: accepted · built

Amends [ADR 0016](0016-packaging-and-distribution.md), which decided ffmpeg is
"documented, not bundled" and left the door open in the same breath: *if
bundling ffmpeg for a "batteries-included" build is worth the size and licensing
once there is demand*. This is that revisit.

## Context

ADR 0016 called ffmpeg "the only external dependency, and it is optional". The
first half is still true. The second half is where the reasoning has not
survived contact with a second install.

### What "optional" actually costs

A second household ran LANcast with no ffmpeg the server could find. The
resulting experience is not a missing feature; it is a server that appears to
work and then does not play things.

Nothing can be probed, so `DecideTrack` returns direct play for every file —
correctly, and it says so: *guessing at a transcode for a file we have not
inspected would burn CPU on a hunch*. The file's own bytes go to the browser.
For MP4 carrying H.264 and AAC that is right and costs nothing. For anything
else the browser is handed data it cannot decode.

Downstream of that, without the tools:

- Any file needing a remux or an encode answers `503` — the honest error, but
  the honest error for most of a real library.
- **Live TV is entirely unavailable**, since every channel goes through ffmpeg.
- Embedded subtitles cannot become WebVTT; embedded cover art cannot be
  extracted; HEIC pictures cannot be decoded for thumbnails.
- No durations and no resolutions, because both come from the probe — which
  also means the resolution filter shipped in v0.6.47 offers nothing at all.

### The failure that made this urgent

The reported symptom was **"AC-3 is not supported yet"**, and it is worth
following, because it is wrong in an instructive way.

AC-3 support shipped in v0.6.45: `delay_moov` let the MP4 muxer write a header
for AC-3 and E-AC-3 instead of ffmpeg exiting before the first byte. But the
desktop client is WebView2, so it uses the conservative `browser` profile, which
deliberately does not claim `ac3`/`eac3` — Chromium cannot decode it. The path
for an AC-3 file is therefore *copy the video, re-encode the audio to AAC*: a
few percent of a core, and utterly dependent on ffmpeg.

So a user with no ffmpeg experiences a **shipped feature as an unshipped one**,
and reports it as a codec gap. The dependency does not degrade into a missing
button; it degrades into wrong conclusions about what the software does.

### And it can be invisible

`internal/mediatools` exists because a Windows service runs as LocalSystem,
whose PATH does not include a per-user ffmpeg install. The tools are present and
unfindable, every item stays unprobed, and nothing reports it. ADR 0016
anticipated this and answered it by recording the directory — which works, and
requires the operator to know there was something to record.

## Decision

**Fetch the media tools on request from inside the app. Do not bundle them, and
do not fetch them unasked.**

### Fetched, not bundled

The installer stays at its current ~17MB rather than 60–100MB, LANcast keeps
distributing only its own binaries, and — the reason that decides it — **an
existing install is fixed without reinstalling.** Bundling helps the next
download; the household that already has the problem is the one that reported
it.

### On request, and never automatically

No download on first run, no "helpfully" reaching out when a probe fails. A
media server that contacts the internet without being asked has broken *no
phone-home*, and that principle does not have an exception for convenience.
Admin-only, for the same reason adding a channel source is: this makes the
server fetch and execute a binary.

### Pinned build, verified before it is unpacked

One pinned URL per platform and a checksum recorded beside it, checked before
anything is extracted or moved into place. **No caller-supplied URL** — a server
that fetches an address the request chose is the server-side request forgery the
channel-source and guide endpoints already refuse, and here the payload is an
executable.

A version bump is therefore a code change, deliberately. A media server that
silently follows a "latest" pointer to a new ffmpeg is a media server whose
playback behaviour changes without a release.

### Into the data directory, not the install directory

`Program Files` is not writable by a service account or by a non-elevated
desktop process, so the tools land beside the database in the data directory,
and the path is persisted the way the service config already records one.

The lookup meets it there: `mediatools` now searches beside the server
executable and a `tools` directory before consulting PATH, so a downloaded,
bundled or hand-dropped copy all resolve identically and none of them depend on
PATH. That also retires the LocalSystem trap rather than documenting it again.

### A GPL build, named as such

The Windows builds that matter are GPL, because they carry x264 — and x264 is
what makes software H.264 encoding possible. An LGPL build would leave
transcoding dependent on a hardware encoder being present, which converts a
CPU cost into a hardware requirement and would make the same file playable on
one machine and not another.

We fetch rather than redistribute, so LANcast's own installer carries no GPL
obligation. The UI states what it is about to download, from where, and under
which licence, before it does it. A download the user cannot identify is not
consent.

### A partial install reports as absent

Verify, then move into place atomically. Anything interrupted leaves the tools
reading as *not installed*, never as installed-and-broken. This is the same rule
the update staging follows, and for the same reason: a half-written toolchain
that reports present turns one clear failure into an unbounded set of confusing
ones.

## Alternatives considered

**Bundle in the installer.** The best experience, works offline, no network at
all. Rejected for now on three counts: it multiplies the download for the
majority who direct-play, it makes us a redistributor of GPL binaries with the
licence text and source offer that implies, and it does nothing for anyone
already installed. Worth revisiting if an offline install becomes a real
requirement rather than an imagined one — at which point this ADR is the thing
to amend, and the download plumbing it describes is not wasted.

**Fetch during setup, from the installer.** Small installer, decision at the
natural moment. Rejected: it helps nobody who is already installed, and a failed
download halfway through a setup is a state that has to be recoverable from
inside an installer, which is the worst place to write that code.

**Keep it documented only.** The status quo. Rejected because the status quo
produced a working install that could not play most of its library, and a
support conversation about a codec that was already supported.

## Consequences

- The install needs network access at the moment it is asked for. An air-gapped
  server still needs the manual route, which is now a file drop into the LANcast
  directory rather than a PATH exercise.
- We own a pinned URL and a checksum, and both will rot. That is accepted: a
  stale pin fails loudly with a checksum mismatch or a 404, and neither silently
  changes what playback does.
- The download is large enough to need progress and cancellation, so this is a
  visible operation with a running report, not a spinner. Measured on the
  pinned build: **160MB compressed**, and `ffmpeg.exe` alone is **144MB**
  unpacked — which is also the retroactive argument against bundling, since
  the estimate in this ADR's alternatives was low.
- Windows first. Linux and macOS have package managers that do this better than
  we will, and the lookup already covers what they install.
- The Media Tools row in Settings becomes the one place that answers "can this
  server play things", which is the question behind most of the reports that
  led here.
