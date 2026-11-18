# ADR 0125 — A TV pairs by code, and a Roku is sent segments it can hear

Date: 2026-11-18 · Status: **accepted** 2026-11-18 · **amended** 2026-11-18
(two corrections found building §1–2; see the end)

The plan is [roku-plan.md](../roku-plan.md), step 1. Step 0's spikes ran on a
Roku TV on 2026-11-17, and their results are recorded in the plan. This ADR
decides the four things they left open: how a TV signs in, how it trusts a
server on the LAN, what it is sent, and where its code lives.

Builds on [ADR 0061](0061-an-api-key-is-not-a-session.md) (API keys), amends
nothing in [ADR 0050](0050-a-converted-file-is-delivered-as-segments.md) for
any client that exists today, and leaves
[ADR 0086](0086-a-tv-browses-as-somebody.md) (DLNA) as it is.

## Context

A Roku app is a thin client against the documented API, the first one LANcast
does not draw in a browser engine. Step 0 established four things.

1. **Signing in needs an API key, and nobody should type one.** An API key
   presented as `Authorization: Bearer` works (ADR 0061), and it is the right
   credential for a TV because it never reaches an admin route. But typing a
   43-character key, or a password, with a remote is the worst part of every
   TV app.
2. **The LAN needs a pinned certificate, and pinning works.** Roku's video
   player refused LANcast's self-signed certificate with only its own CAs, and
   accepted it when the app handed it the certificate
   (`setCertificatesFile`). So the check is real, and the app can pin. What is
   missing is a way to *learn* the certificate that an attacker on the same
   network cannot substitute.
3. **Every converted stream was silent.** LANcast's HLS carries audio and video
   in the same fMP4 segments, and this Roku plays the picture of such a stream
   with no sound. A lab of one clip packaged four ways settled it: fMP4 with
   audio and video together is silent with or without a master playlist, and
   MPEG-TS with audio and video together, or fMP4 with the audio as a separate
   rendition, both play sound. A direct-played MP4 has sound.
4. **The device answers what it decodes.** H.264 High up to level 4.1, HEVC
   Main and Main 10, MPEG-2; AAC, MP3, AC3, EAC3, FLAC, ALAC, Opus, Vorbis and
   LPCM; no DTS, no TrueHD. Cheaper Rokus answer less. An H.264 + AAC Matroska
   file played silent through Roku Media Player over DLNA, which the decoder
   list does not explain and has not been tested directly.

## Decision

### 1. A TV pairs with a code it shows and a person enters

The usual device flow, with three routes and one screen.

**`POST /api/devices/pair`**, with no session. Body `{"name": "Living room
Roku"}` (1–64 characters, trimmed). The server answers:

```json
{"pairing": "<opaque, 32 random bytes>", "nonce": "<16 random bytes>",
 "expires_in": 600, "interval": 5}
```

The TV derives the code it shows from `nonce` and the certificate it was
served (section 2); the server derives the same code from the certificate it
presented, and files the pending request under it.

**`POST /api/devices/pair/poll`**, with no session. Body `{"pairing": "…"}`.
`202` while nobody has approved it; `200` with
`{"key": "…", "key_id": "…", "user": {…}}` once somebody has, exactly once;
`410` once it is denied, expired, or already collected. The TV polls no faster
than `interval`, and a faster poll is answered `429`.

**`POST /api/devices/pair/approve`**, signed in (session, never a key, so a
paired TV cannot pair another), CSRF-checked like every state-changing route.
Body `{"code": "…", "approve": true|false}`. Approving creates an API key **for
the person approving**, named after the device (`Roku: Living room Roku`), and
hands it to the waiting poll. `GET /api/devices/pair/{code}` shows the
approver what they are about to approve: the device's name, the address it
asked from, and when.

The screen is a route in the web client (`/pair`), reachable from Settings ›
Security, where the key then appears and is revoked like any other.

Rules, each because of an obvious attack:

- **Pending pairings live in memory, never in the database.** Ten minutes,
  single use, gone on restart. Nothing about an unfinished pairing is worth
  keeping.
- **The unauthenticated route is rate-limited** per address, and the number of
  pending pairings has a ceiling (32). It exists before anyone signs in, so it
  is the cheapest thing on the server to flood; flooding it should cost the
  flooder more than the server.
- **A code is 8 characters from a 32-letter alphabet with no 0/O or 1/I/L**,
  shown and typed in two groups of four. 40 bits, guessed at the approval
  route only by somebody already signed in, inside ten minutes.
- **The key is the approver's, and ADR 0061 applies to it unchanged.** Hashed
  at rest, shown once (to the TV, never to the approver's screen), never an
  admin route whoever owns it, and revoked in Settings by name. A child
  approving their own TV gets a key that sees what the child sees.
- **Only where the loopback rule already allows the network.** An unsecured
  server binds loopback only; the routes add no new exposure because there is
  no network for them to be exposed on until a password is set.

The known weakness of every device flow is phishing: somebody shows a person
a code and asks them to type it in. The approval screen says plainly that it
grants *this account* to *that device*, and names the device and its address.
That is the standard mitigation and the one taken here.

### 2. The code is bound to the certificate the TV saw

The TV's first contact with a LAN server cannot be verified: the certificate
is self-signed, and the TV has not met it. If the TV simply trusted whatever
answered, a machine on the same network could sit in the middle, collect the
key at the poll, and keep it.

So the code the person types is not issued by the server. It is
**computed by the TV from what it saw**:

```
code = base32( SHA-256( "LANcast pairing v1" || 0x00 || nonce || cert ) )[:8]
```

where `cert` is the DER of the leaf certificate the pairing connection
presented. The TV reads it from `GET /api/devices/pair/certificate` on that
connection, which returns the leaf the server served to it. The server
computes the same value from the certificate it actually presented, and files
the pending pairing under that code.

If anything sits in the middle, the TV computes its code from the
impostor's certificate, the person types that code, and **the server has no
pairing filed under it**. The approval fails as an unknown code. To get past
this an attacker needs a certificate whose hash, with a nonce chosen after
the connection opened, matches the real one in 40 bits, inside ten minutes.
The person compares nothing. The comparison happens because the code they
copy from the TV is the comparison.

After pairing, the TV pins that certificate for every request and for the
video player, exactly as the spike did. When the server's serving key
changes, the pin breaks, and the TV says so and asks to be paired again.
ADR 0124's signed proof would avoid that, but it binds to TLS keying
material that Roku does not expose to an app.

On the public name, where the certificate is from a public CA and rotates
every two or three months, the binding still applies at pairing time and the
TV does **not** pin afterwards; it trusts Roku's CA bundle for that name. The
TV decides which by trying the CA bundle first: a name that verifies with it
is a public name.

The step 3 skeleton has to confirm one thing before this is built on: that
BrightScript can make the first, unverified request
(`EnablePeerVerification(false)` on `roUrlTransfer`) and hash the returned
bytes (`roEVPDigest`, SHA-256). Both are documented; neither has been run on
the TV.

### 3. A `roku` profile, and Roku is sent MPEG-TS segments

**A new profile, `roku`**, beside `browser`, `safari`, `tv` and `native`:

- containers `mp4`, `mov`, `mpegts`, `mp3`, `flac`, `wav`, `aac`;
- video `h264`, `mpeg2video`;
- audio `aac`, `mp3`, `ac3`, `eac3`, `flac`, `alac`, `pcm_s16le`, `pcm_u8`.

That is what every current Roku plays. The app widens it with the claims it
asks the device about, using the **existing** closed set: `hevc` and
`hevc10` when `CanDecodeVideo` says so, `opusmp4` and `flacmp4` likewise.
`knownCapabilities` gains nothing. `matroska` is not claimed until the
Matroska file that played silent over DLNA has played with sound directly in
the app; until then an MKV is remuxed.

**An H.264 level ceiling.** The profile carries a maximum H.264 level (4.1
for `roku`; none for the others, so nothing else changes). A file above it
is transcoded. Level is in ffprobe's output already, and the decision gains
one named test per side of the line, in `decide_test.go`'s style.

**MPEG-TS segments for this profile.** A profile names its segment format,
`fmp4` for every existing profile and `mpegts` for `roku`. For `mpegts` the
muxer is told `-hls_segment_type mpegts`, segments are `segNNNNN.ts`, and
the playlist has no `EXT-X-MAP`. The server-written VOD playlist
(`vodplaylist.go`) and segment-name validation take the same switch. The
segment format is part of what identifies a session, so a TS request never
joins an fMP4 session for the same file, or the reverse.

Chosen over fMP4 with a separate audio rendition, which also had sound in the
lab, because:

- **It is a switch, not a second pipeline.** A separate rendition means a
  master playlist, two media playlists, two segment sequences and two
  readiness checks, all of them written by the server for VOD
  (`vodplaylist.go`). MPEG-TS is the same single-playlist path with a different box.
- **The second benefit of separate audio is one LANcast does not use.**
  An audio rendition gives a player its own track list, but LANcast chooses
  the track on the server (`?audio=`) and starts a new session to change it,
  for every client. The Roku app does the same.
- **The lab proved it on this device**, with the audio LANcast would send.

The cost is known: TS carries a few percent more overhead than fMP4, and
**HEVC in TS on Roku has not been tested.** Before step 2 is merged, a
Main 10 HEVC remux in TS segments plays with sound on the TV, or the
`roku` profile does not grant `hevc` over HLS and HEVC files transcode to
H.264 until it does. FLAC, Opus and ALAC are never copied into TS (ffmpeg
cannot carry them there), so a remux for `roku` with that audio re-encodes
it to AAC. The check that decides what fMP4 can carry gets a TS
counterpart, with a test for each codec it refuses.

No other client's stream changes. `browser`, `safari`, `tv` and `native`
keep fMP4, and the desktop client's tested path (ADR 0050) is untouched.

### 4. The app lives in `clients/roku/`, and CI builds it

In this repository, so an API change and the client that needs it land in
one PR. BrightScript and SceneGraph, compiled and linted by BrighterScript
from npm in a `Roku` CI job beside Go and Client, which fails on any error.
Logic that is not drawing (the claims, the pairing code, the poll,
progress) is written so it runs in tests without a TV.

The app finds a server by typed address, or through DLNA's SSDP
announcement when an administrator has turned DLNA on. **LANcast announces
nothing new for it**: ADR 0086 made announcing a decision an administrator
takes, and a Roku app is not a reason to take it for them.

## Consequences

- Three new routes, one new web screen, one new profile, a level ceiling,
  and a segment-format switch. Each goes in `docs/api.md` and
  `docs/openapi.json` in the commit that adds it.
- `?profile=roku` becomes part of the public API, and any third-party TV
  client with the same silent-fMP4 problem can use it, or ask for a
  `segments` choice later under its own ADR. This one does not make segment
  format a request parameter: it is a property of a device, which is what a
  profile already describes.
- Pairing works for any device that can show eight characters and hash a
  certificate. It is written for Roku and not limited to it.
- A changed serving certificate means pairing the TV again. That is rare on
  the LAN (the certificate is long-lived) and does not happen on the public
  name, which is not pinned.
- DLNA stays what step 0a found it to be: a stopgap with no transcoding, no
  track choice and a pause after every seek. It is not changed to suit Roku
  Media Player.

## Rejected

- **Typing a key or password on the TV.** Possible, and what step 0 did. It
  is the worst part of every TV app, and a password typed on a TV is a
  password a session would then hold.
- **Trust on first use, with a fingerprint for the person to compare.**
  ADR 0124 already found that a 44-character comparison is one people stop
  reading. Binding the code removes the comparison rather than shortening it.
- **fMP4 with a separate audio rendition.** It plays, and it is the modern
  shape. Rejected for now on cost, above; nothing here prevents a later
  profile from asking for it.
- **Making the segment format a query parameter.** It belongs to the device,
  and a parameter is one more thing for every client to get right.
- **A LANcast-specific SSDP announcement for the app.** See ADR 0086.

## Amendments

**2026-11-18, building §1–2.** Two things the text above got wrong, corrected
here rather than edited in place so the original reasoning stays readable.

1. **The TV pins before it pairs, not after.** As written, the TV reads the
   certificate unverified and pins it once paired. A machine in the middle
   that *relays* `/pair/certificate` from the real server would then let the
   TV compute the right code, and read the key out of the unpinned poll. So
   the TV pins the certificate it read for every request from then on, the
   pairing start and every poll included. A relay then has to present the
   real certificate, which it cannot without the real key, and an impostor
   that serves its own gets a code the server never filed. The binding was
   always meant to cover this; the order is what makes it.
2. **The alphabet is Crockford's base32.** There are only 31 letters and
   digits once 0, O, 1, I and L are all removed, so "32 with none of them"
   cannot be built. Crockford's answer is used instead: show 0–9 and A–Z
   without I, L, O and U, and read a typed O as 0 and I or L as 1. Nothing
   shown is ambiguous and nothing a person types for it is refused.
