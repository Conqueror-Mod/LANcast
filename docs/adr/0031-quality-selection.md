# ADR 0031 — Quality selection, and who is allowed to ask for less

Date: 2026-08-13 · Status: accepted

## Context

Until now the delivery decision has had exactly one input: *what can this client
decode?* `?profile=` names a floor and `?can=` widens it, and both answer the
same question in the same direction — the client tells the server what it is
capable of, and the server does the least work that produces something playable.
[ADR 0012](0012-probe-before-transcode.md) is built entirely on that question.

There is a second question it cannot express: *what is this client willing to
receive?* They are not the same. A laptop on a hotel uplink can decode the 4K
HEVC file perfectly and still cannot pull 60 Mbps across the wire. Under the
capability model that file direct-plays, because every claim the client makes is
true, and the playback stalls forever with the server sitting idle — the one
failure mode probing was supposed to eliminate, arrived at from the other side.

Every media server grows this control. The risk is in *how*, because the
capability negotiation it sits next to has a safety property that is easy to
destroy by accident: a request can only ever tell the server it can do **more**,
never less. That is what makes an unrecognised claim safe to ignore and an
absent parameter safe to treat as "no opinion".

## Decision

### A ceiling is a separate parameter, and it only ever narrows

`?max_height=` and `?max_bitrate=`, resolved in `clientProfile` alongside
`?can=` and applied **after** it.

Two parameters rather than one knob, because they move in opposite directions
and a single mechanism that did both would be a mechanism that could be argued
in either. `?can=` widens: it adds codecs and containers, and the worst a wrong
claim can do is produce a failure only that client sees. A ceiling narrows: it
can force an encode that would not otherwise have happened, and the worst it can
do is cost the server CPU. Neither can ever produce the dangerous outcome —
serving a client something it cannot decode — because neither can subtract from
what the profile allows or add to what the client claimed.

Where a named profile carries its own ceiling, the lower wins. A profile capped
at 720p for a reason cannot be talked up to 1080p by a query string.

### A ceiling is not a target

A file already under the ceiling is untouched. This is the rule that keeps the
control honest, and it has two halves:

- **No upscale.** `max_height=1080` against a 720p file must not scale it up.
  That is an encode that adds no detail, costs more bandwidth than the source,
  and is the literal opposite of what the person selecting a lower quality
  asked for. A limit read as a request is a limit that makes things worse.
- **No slack rate control.** `max_bitrate=8000000` against a 3 Mbps file adds a
  constraint that can never bind. It would turn a direct play into a full
  re-encode to enforce a limit the file already satisfied.

Together they mean the default — no ceiling — and a generous ceiling produce the
same behaviour for most of a library, which is what makes the control safe to
leave set.

### The target travels on the decision, not the profile

`Decision.TargetHeight` and `Decision.TargetVideoBitRate`, set only when
`VideoAction` is `encode` and a ceiling actually constrained it.

ffmpeg is built from the decision. Putting the ceiling only on the profile would
mean the two places that construct the command line each re-deriving it from a
request, which is precisely the drift `clientProfile` exists to prevent. And a
copy carries no target at all: a remux re-encodes nothing, so no ceiling reached
a pixel, and reporting one would tell a client a cap applied to bytes that were
passed through untouched.

### The client stores it per device, not per user

[ADR 0006](0006-playback-state-keyed-by-user.md) keys playback *state* to the
user, because where you are in a film is a fact about you and should follow you
to the next screen. A quality ceiling is a fact about the **link** — how much
bandwidth there is between this screen and the server — and roaming it would
carry the hotel wifi setting home to the gigabit LAN and quietly re-encode
everything. It lives in `localStorage`, where `lancast:volume` and the
codec-denial list already are.

The same reasoning covers the rest of the playback settings panel: the audio
output device cannot even be named on another machine, and subtitle size is a
question about viewing distance.

## Consequences

Changing quality mid-film reloads the source, because it is a new question for
the server rather than a client-side switch. It resumes at the live position
rather than the last-saved one, so it reads as a short reconnect — the same
interruption a transcode seek already costs — instead of a jump backwards.

Selecting a capped quality can turn a direct play into a full re-encode of a
file the client could have played untouched. That is the control working, not a
regression, but it means a ceiling left set on a fast connection makes the
server work for nothing. Hence Original as the default and the first rung: on a
LAN, which is what this project is, no ceiling is the right answer almost
always.

The ladder pairs a resolution with a bitrate rather than offering them as two
controls. Independent knobs make "1080p at 1 Mbps" reachable, which is a worse
picture than 480p at the same rate and reads at the player as a broken server.

Nothing existing changes. A request without the parameters produces exactly the
decision it did before, which is what makes this additive under
[ADR 0018](0018-api-contract-and-versioning.md).
