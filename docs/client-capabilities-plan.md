# Let the client say what it can play

**Status: planned, not built.** Prompted by a measurement, and it changes an
assumption two ADRs lean on.

## The measurement

A Chromium engine on the development machine, asked directly:

```
hevc (hvc1)   canPlayType: probably   MediaSource.isTypeSupported: yes
hevc (hev1)   canPlayType: probably   MediaSource.isTypeSupported: yes
h264          probably                yes
av1           probably                yes
ac3           no                      no
eac3          no                      no
```

**HEVC decodes natively here.** LANcast's `browser` profile lists
`h264, vp8, vp9, av1` and excludes it, so every HEVC file gets a full video
re-encode on a client that could have played the bytes as they are.

That is not a small tax. On this library it is the difference between a film
starting instantly and one taking many seconds to produce a first frame while
ffmpeg encodes ahead of the player — and it is what "laggy between films" turned
out to mean. Files that direct-play (Aladdin) started at once; the HEVC ones did
not.

## Why the profile is wrong, and why it was right

From [docs/api.md](api.md):

> `browser` excludes HEVC deliberately: Chrome's support is conditional on
> hardware and Firefox has none, so claiming it for an unidentified client
> trades a cheap remux for an unexplained failure. Clients that know better say
> so.

That reasoning is still correct. The problem is the last sentence: **no client
ever says so.** The mechanism exists — `?profile=` on
`/api/items/{id}/playback` and on the stream endpoints, resolved by
`probe.ProfileByName` — and the React client has never sent one, so every
browser in the house is treated as the floor.

The server is guessing on behalf of a program that *knows the answer* and is
sitting right there. That is the thing to fix.

## The design

**The client measures itself and sends the result.**

`canPlayType` and `MediaSource.isTypeSupported` answer per codec, in the exact
engine that will do the decoding, on the actual machine. The client asks once
per session, caches the answer, and sends it with the playback request.

### Additive, not a replacement

The client sends what it can do *beyond* the floor, not a whole profile:

```
GET /api/items/87/playback?can=hevc,ac3
```

The server starts from `browser` and adds only what it recognises. Three
reasons:

- an unknown or absent value keeps today's behaviour exactly, so this is
  additive under [ADR 0018](adr/0018-api-contract-and-versioning.md) and a
  client that never learns about it is unaffected;
- the floor stays the floor, so a bug in detection can only ever *fail to
  improve* things, never claim less than a browser can already do;
- the named profiles keep working for clients that prefer them (`safari`, `tv`),
  and this is not a third way of saying the same thing — it is the same profile
  machinery with a delta applied.

### The trap: the decision and the stream must agree

`/api/items/{id}/playback` decides, and `/api/stream/{id}/transcode` decides
*again* from its own `?profile=`. Send capabilities to one and not the other and
they disagree — the client is told "direct play" and then asks for a transcode
that re-derives a different answer, or the reverse. **Whatever the client sends
to one, it must send to both**, and that belongs in one place in the client
rather than at each call site.

### The fallback, which is the part that matters

`"probably"` is a claim. HEVC decode depends on the GPU, the OS, and on Windows
sometimes a store extension; the claim can be wrong, and when it is wrong the
result is a black screen — the silent failure this project keeps writing
postmortems about.

So: **if a direct-played source errors or never produces a frame, retry it as a
transcode and remember.** The remembering is what stops it being an infinite
loop and a permanently worse experience: the capability that lied is dropped for
that browser profile, persisted locally, and the client stops claiming it.

A visible note when that happens ("this file could not be played directly;
converting") is better than a silent downgrade, because a machine that lies
about HEVC will do it for every HEVC file and the operator should find out from
the app rather than from a stopwatch.

## What this does to two ADRs

**[ADR 0013](adr/0013-transcode-pipeline.md) is unaffected.** The decision
engine is unchanged; it gains a caller that fills in the profile honestly.

**[ADR 0023](adr/0023-native-desktop-client.md) stage 2 gets weaker, and should
say so.** Its case for libmpv rests substantially on the codec tax:

> A desktop client that decodes what ffmpeg decodes direct-plays HEVC, 10-bit,
> AC-3, DTS, TrueHD, MKV — the cases that today force a transcode or a remux.

HEVC is the largest item on that list, and if a capable browser already plays it
the argument loses its biggest example. What remains for stage 2 is real but
smaller: AC-3 and E-AC-3 (measured `no` above), DTS, TrueHD, and MKV as a
container. That is a narrower case than the ADR currently makes, and the ADR
should be amended rather than left to read as if nothing had been learned.

**This does not make stage 1 pointless.** Owning the window was never mostly
about codecs — it was cached redirects, certificate walls, and the handoff with
no good failure mode, all of which stand.

## Not in scope

- **Trusting a client's claim for anything but its own playback.** This decides
  what to send to the caller and nothing else; there is no authorization
  question here, and no other user is affected by one browser's answer.
- **Audio capability beyond what the same call returns.** AC-3 and E-AC-3 come
  back `no` on this machine, so nothing changes for them today, but the same
  parameter carries them the day a client says yes.
- **Re-probing the library.** This is a decision-time change; nothing stored
  changes, and no rescan is needed.

## Verification

The decision rules are tested against fixtures with no ffmpeg and no media
([CLAUDE.md](../CLAUDE.md)), and this must stay that way: the profile-with-delta
resolution is a pure function and gets one named test per capability, the same
shape as `internal/probe/decide_test.go`.

Then the part fixtures cannot show, on the real library:

- an HEVC film **starts immediately** and the server logs no transcode session
  for it;
- a file with AC-3 audio still transcodes its audio, because the browser said
  `no` and the server believed it;
- the fallback fires when the claim is wrong — testable by hand by claiming a
  capability the machine does not have, which should end in a converted stream
  and a note, not a black screen.

The last one is the one to actually perform rather than reason about. Everything
in this document except the measurement is a prediction.
