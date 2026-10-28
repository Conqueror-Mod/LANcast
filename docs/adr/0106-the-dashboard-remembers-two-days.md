# ADR 0106 — The dashboard remembers two days

Date: 2026-10-27 · Status: **accepted** · Builds on [ADR 0096](0096-metrics-have-their-own-key.md)

## Context

Settings › Dashboard shows what is happening now: conversions running and the
audit log. The roadmap asked for a dashboard **with history**, the answer to
"it was slow last night, what was going on?". The metrics endpoint
(ADR 0096) already has the numbers, but Prometheus is for people who run
Prometheus, and a household server's owner mostly does not.

## Decision

**A reading every minute, kept 48 hours.** Each sample holds:

- people here watching something (presence);
- conversions running, re-encoding or only rewrapping (the transcode manager);
- the whole machine's CPU, from `GetSystemTimes` on Windows and `/proc/stat`
  on Linux. Elsewhere it is `-1`, and the chart says it cannot be read rather
  than drawing zeros;
- the memory this program holds, from `runtime/metrics`, without the
  stop-the-world pause `ReadMemStats` takes;
- requests and unexpected server errors during the minute, as deltas of
  ADR 0096's counters.

All of it comes from what the server already keeps. Nothing new is counted.

**Whole-machine CPU, not this program's.** Conversions run in ffmpeg, a child
process, so this program's own CPU would read idle while the machine was
flat out converting. "Is the computer busy" is the question.

**In memory, saved beside the database.** `history.json` is written every five
minutes and at shutdown, atomically, and loaded at startup, keeping only what
is inside the window, so a restart does not wipe the night before. **No
schema change**: two days of minutes is about 3,000 small records, and the
history is a convenience. A lost or broken file is an empty history, not an
error. The recorder runs on the context cancelled on every way out, including
the exit that finishes an update, and in the workers group, so its last save
completes before the database closes.

**No bandwidth.** The server counts the bytes conversions hand out and nothing
else. Counting every response means wrapping the response writer, which
ADR 0096 declined because the event stream needs its `Flush`. The page says
network use is not shown rather than showing a partial number as a whole one.

**Small multiples, one axis each.** Six charts, one measure each: never two
scales on one chart. One series per chart, so the title names it and there is
no legend. A 2px line in a blue from the field (`#5f8ee0`, validated against
the panel surface: lightness inside 0.48–0.67, contrast at least 3:1), never
gold, which means *where you are*. Lines break where the server was off
rather than drawing across the gap. There is a crosshair and tooltip on
hover; the conversions tooltip splits re-encoding from rewrapping. A table of
hourly averages and peaks gives the same numbers without the picture. 6, 24
or 48 hours. Refreshed each minute while the page is open.

**Administrators only.** `GET /api/dashboard/history?hours=N`, 1 to 48, like
the rest of the dashboard.

## Consequences

- Answers "what was the server doing at 2 a.m." from the page that already
  answers "what is it doing now".
- A server that runs for under a minute records nothing, and says so.
- Bandwidth would need its own decision: counting bytes at each streaming
  route, not wrapping the writer.
