# Logging — plan

Two incidents on 2 October came down to the same blind spot. The server logged
the wrong thing too often: a paired server that was switched off wrote
3,263 identical lines in five days. And it logged the right thing never: a
login refused for twenty minutes left no trace at all, so its cause can't be
established. This plan is about making the log answer the questions people
actually bring to it, **"why did that happen?"** and **"when did it start?"**,
without making it noisier.

## What already works

- **Rotation and separation.** `internal/applog` rolls each log at 4 MB and
  keeps one previous generation. The server (`lancastd.log`), the tray
  (`lancast-tray.log`) and the desktop window (`lancast-client.log`) each own
  a file, because two processes rotating one file corrupts it. Size is not a
  problem to solve. *Signal* is.
- **A viewer.** Settings shows the tail of the server log and of this window's
  log, so nobody has to be told where the files are.
- **Levels with a switch.** The server runs at Info with a Debug switch
  (`cmd/lancastd/main.go`).
- **Errors.** Every 500 goes through `writeInternal` and logs at Error.
  Conversions log their decision and reason (`transcode started`).
- **State, not repeats.** `peerhealth.go` (v0.9.47) logs a peer going down
  once and coming back once. That is the pattern the rest of this follows.

## The gaps, measured

Log calls per package on main: `api` 77, `cmd/lancastd` 74, `scan` 42,
`transcode` 24, `cmd/lancast` 23, `enrich` 20, `plugin` 17, then single
figures. **None at all** in `together`, `peer`, `presence`, `selfupdate`,
`subtitle`, `playlist`, `games`, `backup` or `mpv`, and only 4 in `probe`.

What that means in practice:

- **Refusals are silent.** A 401, a 403 from the origin check, a 429 from the
  throttle: none were logged. #730 adds login refusals. The origin check
  (`cross-origin request refused`) is the other way a person can be "denied"
  with no record.
- **A film that plays directly leaves no trace.** Only conversions log. "What
  did the server decide for this film, and why?" has no answer for most of
  the library.
- **The web app cannot write to any log.** The desktop window can *read* its
  log (`lancastClientLog`) but not write to it. Playback failures, fallbacks
  to conversion, codec claims withdrawn after a failure (the denial ratchet
  that once silently re-encoded every HEVC film for weeks), the HLS incident
  path, and uncaught JavaScript errors all go to a console nobody has open.
- **Social features are dark.** Watch Together rooms, pairing changes and
  presence failures log nothing, and those are exactly the features that
  involve somebody else's machine, where "what happened" is hardest to work
  out after the fact.

## The rules

**Never log a secret.** Not passwords, session tokens, cookies, API keys,
stream tickets, invite codes, or the certificate pin's private half. Not a
username that matched no account either, since that's as likely to be a
password typed into the wrong box (#730 established this).

**Log a change of state, not a repeat.** The first failure and the recovery
are news. The sixtieth identical failure is Debug. A shared helper makes this
the easy path (Phase 3).

**Levels mean something.**
- **Error:** something is broken and needs a person.
- **Warn:** something was refused, or fell back to a worse path.
- **Info:** a state changed (signed in, room opened, update staged).
- **Debug:** repeats and detail.

**One line answers one question.** It carries the reason in words (`reason=`),
not just a code, and the IDs needed to follow it (`item=`, `user=`, `peer=`).

**Titles and IDs both.** A line about playing a film names it (`title=`) and
carries its `item=`, so the log reads at a glance and still points at the exact
row. Decided 2026-10-02: the log is a local file on the owner's machine, and
being able to read "Dreamcatcher fell back to a conversion" without a database
lookup is worth more than keeping titles out of it. It is still not a second
watch history: playback is logged when it *starts* or *fails*, not as progress.
What stays out is anything a person typed that the server did not accept (see
the first rule).

## Phase 1: the server says what it refused and decided

1. **Refusals.** The origin check, missing or expired sessions on
   state-changing routes, rejected API keys, and the throttle: Warn, with route
   and reason, deduplicated per client (Phase 3's helper, or a small local
   one first). A flood of 401s from a tab with a dead cookie is one line, not
   hundreds.
2. **Sessions.** Signed in (account, client address), signed out, password
   changed (already audited, now also logged), sessions revoked. #730 covers
   refusals.
3. **Playback decisions.** One Info line per playback start: item, method
   (direct / remux / transcode / native), reason, and the claims the client
   sent. Today only conversions log, so direct play is invisible.
4. **The dark areas.** Watch Together (room opened or closed, joins and
   leaves, desync corrections at Debug), pairing (invite created, accepted,
   revoked), updates (check result, download, staged, swapped, failed),
   backups (written, failed), each on state changes only.

## Phase 2: the web app can write to the client log

1. **A write binding beside the read one:** `lancastClientNote(level, area,
   message)`, desktop only. Closed `area` vocabulary (`playback`, `auth`,
   `capabilities`, `error`). The message is capped in length, newlines are
   stripped so the page can't forge log lines, and a per-area rate limit means
   a broken page can't fill the file.
2. **What the page writes:**
   - playback failures and fallback to conversion (with the decision reason);
   - a codec or container claim withdrawn;
   - HLS incidents;
   - "signed out", when a 401 arrives on a page that thought it was signed in;
     that's the moment #730's bug would have been caught;
   - uncaught errors and unhandled promise rejections, with message and top
     stack frame.
3. **Browser tabs** have no binding, so for now they keep the console. A
   server endpoint for client notes is possible later. It is deliberately not
   in this phase, because it is API surface and a write path an unsigned-in
   page could reach.

## Phase 3: keep it quiet as it grows

- **A dedupe helper** (`applog`): "say this once per key per window, then a
  count". The peer fix and the refusal logging both use it, and the next noisy
  path gets it for free.
- **A test per new log line** that it carries its reason and no secret, the
  same shape as `TestRefusedLoginsAreLoggedWithoutThePassword`.

## Not in scope

- **Shipping logs anywhere.** No phone-home is not negotiable. Logs stay in
  their files and leave the machine only if a person copies them.
- **Structured log search in the UI.** The tail view is enough for now.
