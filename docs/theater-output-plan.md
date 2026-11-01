# Theater output plan

Date: 2026-10-31 · Follows item A of
[the Kaleidescape study](kaleidescape-comparison.md). Code read at `main`
after #977.

The desktop player should hand the room the film as it is: at the film's own
frame rate, in its own dynamic range, with its original audio bitstream going
to the receiver. Today it does none of these (`internal/mpv/options.go` sets no
display, HDR or passthrough option). This plan closes all three, plus a
measurement so "nothing waits" can be checked rather than assumed.

Nothing here touches the server or ffmpeg in the service. Everything is the
desktop client: `internal/mpv`, `cmd/lancast/player_windows.go`,
`internal/clientwindow`, and the Player settings pane.

---

## Shape

**Three switches, not one.** The study proposed one "theater output" switch.
The three parts fail independently: a TV may take 24 Hz but not HDR, a PC may
go straight to the TV with no receiver, and an exclusive-mode audio device
silences every other program on the machine. One switch would make the person
take all three trade-offs to get one. So:

| Setting (This device › Player › Theater output) | Default |
|---|---|
| Match the display to the film's frame rate | Off |
| Send HDR to the display | Off |
| Send the original audio to the receiver | Off |

All three are per device, in `playback/prefs`, because they describe the
screen and the cable rather than the person. A server-held setting would
follow you to a laptop with no receiver.

**Each row appears only where it works.** The version-skew rule from
`mpvFeatures` (`player_windows.go`) applies: v0.9.44 showed night mode against
a client that refused it. The client advertises `refresh`, `hdr` and
`passthrough` separately, and the pane shows each row only when its feature is
present.

**Four PRs**, each shippable alone, in this order — cheapest proof first, the
one that needs a library rebuild last:

1. Refresh-rate matching
2. HDR passthrough
3. Audio bitstream passthrough (needs a libmpv rebuild)
4. Time to first frame, logged

---

## 1. Refresh-rate matching

### What it does

When a film starts in the native player, the monitor the window is on switches
to the film's rate, and switches back when playback stops.

| Film | Preferred modes, in order |
|---|---|
| 23.976 | 23.976, 47.952, 119.88 |
| 24 | 24, 48, 120 |
| 25 | 25, 50, 100 |
| 29.97 | 29.97, 59.94, 119.88 |
| 30 | 30, 60, 120 |
| 50 / 59.94 / 60 | the same rate, then double |

Exact multiples only. When the monitor offers none, nothing changes and the
log says why. Then mpv `video-sync=display-resample` covers the last fraction
of a percent (23.976 film against a "23 Hz" mode that is really 23.9760001).

### How

- **The rate comes from mpv, after the file opens.** `container-fps` (falling
  back to `estimated-vf-fps`) is added to `Observed` in `events.go`. The
  server's probe also knows `SourceFrameRate`, but mpv is the source of truth
  for what it is actually about to show, and it costs no API change.
- **The play is held until the display settles.** `Load` already opens paused.
  When the page's `play` arrives before the switch has finished, the native
  side holds it, switches, waits a settle time, then unpauses. TVs take one to
  three seconds to resync HDMI; without the wait, the first seconds of the
  film are lost to a black screen. Default 2 s, adjustable in the row (some
  TVs need longer). No wait when no switch was needed.
- **The mode list comes from `EnumDisplaySettingsEx`** on the window's
  monitor. Windows reports 23.976 as `23` and 59.94 as `59`, so an integer
  rate `n` is treated as `n/1.001` when the film is NTSC-family. After the
  switch, read the real rational rate back through `QueryDisplayConfig`
  (already bound in `displayconfig_windows.go`) and log it. That number is
  evidence; the integer is not.
- **The switch is `ChangeDisplaySettingsEx` with `CDS_FULLSCREEN`** on that
  monitor's device name: temporary, never written to the registry. Same
  resolution, same bit depth, only the frequency changes. The games lesson
  applies: never touch the primary display, only the one the window is on.
- **The restore path:**
  - on `stop`, which the page calls when leaving the player;
  - on app exit;
  - *not* between films of the same rate in a queue, which would be two pointless
    resyncs;
  - when the window moves to another monitor mid-film (restore the old monitor,
    match the new one);
  - **after a crash.** Before switching, write `display-restore.json` in the
    client data dir (monitor device path, original mode). Delete it on restore.
    On startup, a file that is still there means the last run didn't restore,
    so restore from it. Then don't depend on what `CDS_FULLSCREEN` does when a
    process dies: watch it once, by killing the client mid-film.
- **Mini player and PiP keep the film's rate.** It's still playing, and a
  second resync when you dock it would be worse than judder in a small window.

### Pure parts, tested without a monitor

- `chooseMode(film float64, offered []Mode) (Mode, reason)`, a table test per
  row of the table above, plus the "nothing fits" cases, as in
  `decide_test.go`: the decision *and* its reason.
- The restore-file read/write and the "stale file means restore" rule.
- The play-hold state: play before the switch, play after the switch, stop
  during settle.

### Logs

`msg="display matched" film_fps=23.976 from=60 to=23.976 monitor="LG …"` and
`msg="display not matched" reason="no 24 Hz family mode offered"`, so the
first question about judder is answered by `grep`.

---

## 2. HDR passthrough

### What it does

An HDR10 film puts an HDR display into HDR and is shown untouched, not tone
mapped. An SDR film on the same display looks the same as it does today.

### How, and what has to be checked first

- **Name the video output.** `options.go` names `gpu-api=d3d11` but not `vo`.
  The theater PC's log shows mpv 0.41 choosing `gpu-next`, which is what
  `target-colorspace-hint` needs. Pin `vo=gpu-next` in `options.go`, because
  *never let it choose* (the lesson from the ffmpeg section of `CLAUDE.md`)
  applies to mpv just as well.
- **Then `target-colorspace-hint=yes`**, set only when the switch is on. With it
  off, nothing changes from today.
- **Windows' own HDR switch.** mpv can ask for an HDR swapchain. It can't turn
  on "Use HDR" in Windows display settings, and without that the desktop stays
  SDR and Windows maps the picture down. Two options:
  - *a.* Tell the person: the row says "needs Use HDR on in Windows display
    settings", and the player logs `hdr requested, display is SDR` when it
    isn't.
  - *b.* Do what Kodi does and switch it: `DisplayConfigSetDeviceInfo` with
    `SET_ADVANCED_COLOR_STATE` on the window's monitor for HDR films, restored
    the same way the refresh rate is (same restore file, same crash rule).

  **Recommendation: a first, b as a follow-up.** b changes a system display
  setting, and doing that well on the first try is a lot to ask. a is honest
  and costs nothing.
- **Dolby Vision is out of scope** (study, *What not to copy*). Profile 8 files
  carry an HDR10 base layer and play as HDR10. Profile 5 has no base layer, and
  libplacebo tone maps it. The detail page must not claim DV output.
- **ADR 0033 is untouched.** Server-side tone mapping is for clients that need
  it; the native player plays the original file through the relay
  (`/api/stream`), so the two paths don't meet.

### Logs

`msg="video output" vo=gpu-next hdr=pq primaries=bt.2020 display_hdr=true` at
file load, from mpv's `video-params` and `video-target-params`. Those two
properties tell you whether the picture reached the display untouched,
without anyone needing to look at the TV.

---

## 3. Audio bitstream passthrough

### Blocked on a library rebuild

`third_party/libmpv/build.sh` configures FFmpeg with `--disable-muxers`, and
mpv's passthrough (`ad_spdif`) wraps the bitstream with libavformat's **spdif
muxer**. As built today, `audio-spdif` can't work, whatever the options say.

- Add `--enable-muxer=spdif` to the FFmpeg configure line. The muxer is LGPL,
  so the licence position in ADR 0069 doesn't change. Say so in the commit and
  in `PROVENANCE.md`.
- Rebuild, record the new SHA-256 in `PROVENANCE.md`, and attach it as the
  provenance process says. The updater already stages `libmpv-2.dll` with the
  executable (`internal/update/download.go`), so the DLL and the client that
  expects it arrive together.
- **Feature detection.** A hand-copied older DLL is the one way a client gets
  ahead of its library (the dev-build case). Advertise `passthrough` only when
  the loaded DLL's SHA-256 is one recorded as built with the muxer, a short
  list kept beside `PROVENANCE.md`. Hashing the DLL once at first playback is
  cheap, and it reuses the reasoning behind pinning the build at all.

### What it does, once the library can

- `audio-spdif=ac3,eac3,dts,dts-hd,truehd` and `audio-exclusive=yes`.
  WASAPI is already the only audio output in the build (`-Dwasapi=enabled`).
  TrueHD and DTS-HD need exclusive mode, so the two go together.
- **The controls this turns off have to say so.** In passthrough the receiver
  decodes, so LANcast can't change the sound. Volume, mute, night mode and
  dialogue boost do nothing. Leaving them on screen would be the v0.9.44
  mistake again (controls shown that do nothing).
  - Observe `audio-params/format`. In passthrough it reads `spdif-truehd`,
    `spdif-eac3` and so on. `Observed` only reads doubles and flags today, so it
    gains a string format: a small change to `readChange` and its test.
  - The state the page reads gains `passthrough: "truehd"` (empty when PCM). The
    player panel greys out volume, night mode and dialogue and shows one line:
    *"Sent to your receiver as Dolby TrueHD. Use the receiver's volume."*
  - It's **per track, not per setting.** An AAC stereo commentary is decoded as
    usual even with the switch on, and its controls work, because mpv only
    passes through the codecs in the list. That's why the UI reads the live
    format rather than the setting.
  - `applyAF` must not hand mpv a filter graph while passing through. Clear `af`
    when the format is `spdif-*` and rebuild it when it isn't. A filter on a
    bitstream either fails or quietly makes mpv decode it again. Find out which
    from the log, and don't guess.
- **Exclusive mode takes the device.** Windows sounds and other programs go
  silent while a film plays. The row says this. If the device is already held
  by another program, find out what mpv does (falls back to shared PCM, or
  fails to open) by watching it, then write it down. The ffmpeg section's rule,
  *a claim about a failure case is worth having watched*, applies here too.

### Pure parts

- `passthroughOf(format string) string`: format string to codec label.
- `AudioFilter` returns no graph when passthrough is active. It's a new row in
  the existing table test in `audiofx_test.go`.
- The page side: a panel test that volume and night mode are disabled with the
  explanation shown when the state says `passthrough`, and enabled when it
  doesn't. jsdom can check this, because it's wiring, not layout.

---

## 4. Time to first frame

So *nothing waits* is a number, not a feeling.

- On `open`, start a clock. Log it when mpv reports the first frame is shown
  (`playback-time` first advancing after the unpause), with the parts split
  out: relay connect, file loaded, display settle (if any), first frame.
  `msg="first frame" total_ms=… settle_ms=… item=…`.
- Do the same for seeks: command to first frame after it.
- Log only, with no dashboard yet. A week of logs from real use says whether a
  dashboard tile is worth it, and whether the refresh-rate settle needs work.

### And one test the study asked for

**Track choice survives pause and seek.** Kaleidescape owners report subtitles
turning themselves back on after a pause. LANcast draws subtitles in the page
(`nativeTracks.ts`), so this is a provider test. Choose a subtitle track and
turn it off, then pause, resume, seek, and assert it is still off. Audio track
the same. It's cheap, and it rules the bug out for good.

---

## What this needs from Chris

Tests can't see a mode switch, an HDR flag or an Atmos light, so each PR ends
in one sitting at the theater PC. The code and logs are written so that sitting
is short:

| PR | Check | What tells you |
|---|---|---|
| 1 | Play a 23.976 film. Does the TV's info show 24p? Stop it: back to 60? | TV info banner, `display matched` log line |
| 1 | Kill the client mid-film and reopen it | Display back to 60 within a second of reopening |
| 2 | Play an HDR10 film with Windows HDR on | TV shows HDR badge, `video output … display_hdr=true` |
| 3 | Play a TrueHD Atmos film | Receiver shows Atmos; panel shows the receiver line and greyed volume |
| 3 | Switch to a stereo commentary track | Volume works again |

Nothing on this list involves the installed service, so swapping in a test
build and the session-0 rule don't apply. A locally built client does need the
cert-pin setup from memory (`locally-built-client-loses-cert-pin`), or it can't
reach the server at all.

## What the theater PC does today (read 2026-10-31)

From `%APPDATA%\LANcast\client\mpv.log` and the Windows audio endpoints:

- **`VO: [gpu-next] … d3d11[p010]`.** The video output is already `gpu-next`,
  so `target-colorspace-hint` is available with no vo change and no re-proof
  under the overlay. PR 2 still names `vo=gpu-next` in `options.go` so it can't
  drift.
- **Audio goes to `SteelSeries Sonar - Media`**, a virtual mixer, because it is
  Windows' default device. mpv asked for 5.1, and Sonar accepted 7.1 at 96 kHz.
  What Sonar does with it next depends on Sonar's own output, not on LANcast.
- **The home theater is on the onboard Realtek analog line-out**, and Windows
  has that endpoint set to **quadraphonic** (speaker mask `0x33`: front and
  rear, no centre, no LFE). One cable in the green jack carries only front
  left and right. Rear would come out of the black jack, and centre and sub
  have no channels at all. `SK80` is the mic interface's USB device, not the
  home theater.
- **There's an unused `Realtek Digital Output`** (S/PDIF), and no active HDMI
  audio endpoint. The Roku TV (display 3) has HDR but shows no audio device.

That accounts for *"it usually doesn't use all speakers"*, and none of it is a
LANcast setting. Getting 5.1 means one of:

- three analog cables (green front, black rear, orange centre/sub) with the
  endpoint set to 5.1, if the system has 5.1 direct inputs. That's PCM with
  every LANcast control still working, and needs no code;
- S/PDIF, which carries Dolby Digital and DTS 5.1 **only as a bitstream**
  (PCM over S/PDIF is stereo). That makes PR 3 the fix, limited to `ac3`,
  `eac3` (sent as AC-3) and DTS core. TrueHD and DTS-HD need HDMI.

Which one applies depends on the system's inputs. **Answered: the system is a
GPX HT219B**, a 5.1 DVD home-theater-in-a-box. Going by its manual, the only
way in from outside is one stereo "Auxiliary Input". The coaxial jack is a
digital *output*, and there are no 5.1 analog or HDMI inputs. So nothing from
the PC, LANcast included, can give it discrete 5.1. The useful change is to set
the Realtek endpoint to **Stereo**, so the rear channels are folded into the
two that reach the box rather than going to an empty black jack. **PR 3 goes to
the back** until the room has a receiver that takes HDMI or S/PDIF.

**Also for PR 3:** passthrough must name its output device, not follow
Windows' default. Following the default would send a bitstream into Sonar, and
Sonar can't pass it on. The row gets a device picker (mpv `audio-device`,
listed from `audio-device-list`), saved per device like the rest.

## Open questions

1. ~~Which inputs does the home theater system have?~~ One stereo aux input
   (above). PR 3 waits for a receiver.
2. **HDR switch, a or b?** This plan recommends a (tell the person) first.
3. **Settle default.** 2 s is a guess. The first evening's logs will say
   whether it's right for the TV in question.

## Rules this has to respect

- *No phone-home*: nothing new talks to the network. The relay stays the only
  thing mpv reads.
- *Process execution stays split from parsing*: every decision above (mode
  choice, restore rule, passthrough label, filter-graph suppression) is a pure
  function with a table test. The Windows calls are a thin layer, as
  `player_windows.go` already is.
- *Gold is earned*: the passthrough line and the disabled controls use the
  muted text treatment, not gold.
- *No API change*: everything is between the page and its own window, so
  `docs/api.md` and `openapi.json` don't move. If a later step reports
  passthrough or the matched rate to the server (for the dashboard), that is a
  contract change and gets both.
