# ADR 0048 — The media tools install themselves on first run

Date: 2026-08-27 · Status: **proposed**

Amends [ADR 0043](0043-media-tools-are-fetched-not-bundled.md), which decided
the media tools are fetched **on request and never automatically**, and said so
in those words. This proposes reversing that half of it. Everything else in 0043
— pinned URL, checksum before unpack, data directory, GPL build named as such,
atomic move, partial install reads as absent — is unchanged and load-bearing
here.

It also requires a change to [README.md](../../README.md), which is why this is
an ADR and not a setting.

## Context

### What 0043 fixed, and what it did not

0043 was written because a household ran LANcast with no ffmpeg the server could
find, and experienced *a working install that could not play most of its
library*. It fixed the **capability**: there is now an install button, it
fetches a pinned build, it verifies it, and the lookup finds the result without
a PATH exercise.

It did not fix the **discovery**, and the discovery was the actual failure. The
report that prompted 0043 was not "I cannot find the install button". It was
**"AC-3 is not supported yet"** — a user reaching a conclusion about what the
software does, from a symptom two layers away from its cause. A button they
never went looking for does not answer that, because they did not know there was
a question.

Three things make the button hard to arrive at:

- It is in **Settings → Metadata**, which is not where someone goes when a film
  will not play.
- It is **admin-only**, deliberately, so a member on a family server cannot even
  see the thing that would fix their playback.
- It is only reached by someone who has already worked out that the problem is a
  missing tool, which is the step the original reporter did not take.

### The failure has a shape, and it is the shape of the whole session that led here

Nothing errors. Probing quietly returns direct play for every file, because
guessing at a transcode for a file nobody inspected would burn CPU on a hunch —
which is correct. The file's own bytes go to the browser. For MP4/H.264/AAC that
works. For everything else the browser is handed data it cannot decode, and the
picture simply does not appear.

This is the same class as the two defects found on 2026-08-26 — a codec denial
that lasted for ever, and a muxer flag corrupting live timestamps. In all three
cases the software behaved *correctly* at every step and the outcome was wrong,
so there was nothing for a log to say and nothing for a test to catch. What they
have in common is that the person affected has no way to tell a degraded install
from a working one.

## What "no phone-home" actually protects

The principle in README.md reads:

> **No phone-home.** Local-first, LAN-first. Nothing is required to reach the
> internet for the server to work. Remote access is opt-in and self-owned
> (WireGuard, Tailscale, your own reverse proxy) — never a relay you rent.

Read closely, it is doing three separate jobs, and this proposal collides with
exactly one of them.

**It protects against your data leaving.** A media library is an unusually
revealing dataset. Nothing here sends any: a tools fetch is a GET for a pinned
URL, and what reaches the far end is an IP address and a request for a file that
is identical for every LANcast install on that platform. This job is untouched.

**It protects against a dependency you do not own.** No relay, no vendor account,
no service that can be withdrawn or start charging. A one-time fetch of a
third-party binary creates no ongoing dependency: the server works before it,
after it, and for ever afterwards without repeating it. This job is untouched.

**It protects against the server doing network things you did not ask for.**
This is the one. A first-run install is unrequested outbound traffic, and 0043
named it precisely: *"A media server that contacts the internet without being
asked has broken no phone-home, and that principle does not have an exception
for convenience."*

That sentence is still right about what it describes. The question this ADR puts
is whether the third job, as written, is worth what it currently costs — and
whether "without being asked" is the same as "without being told".

## Decision (proposed)

**On first run, the server fetches the media tools once, having said so, unless
told not to.**

### It is disclosed before it happens, not after

First run already has a human at it: the server binds loopback only until a
password is set ([ADR 0011](0011-single-password-with-server-sessions.md)), so
account creation is a moment where somebody is present and reading.

The setup screen states what will be downloaded, from where, how large it is
(**160MB compressed; `ffmpeg.exe` is 144MB unpacked**), under which licence, and
that it can be declined — with a control to decline it, on that screen, before
the fetch starts. 0043's own rule stands unchanged: *a download the user cannot
identify is not consent*.

This is the load-bearing difference from what 0043 rejected. 0043 refused a
server that "helpfully" reaches out **silently**. What is proposed is a server
that reaches out **having announced it to a present human who can stop it**.
Whether that distinction is real enough to carry the principle is the decision
being asked for, and it should be argued rather than assumed.

### Once, never on a schedule, never on failure

One attempt, on first run, and never again unattended. Specifically **not** on a
later probe failure, which is the "helpfully reaching out" 0043 rejected and
which this ADR does not revisit: a failure is a bad trigger because it recurs,
and a fetch that recurs is the retry loop this project has already learned to
refuse in two other places.

If the fetch fails, the tools read as absent — 0043's rule — and the existing
button is how it is retried, by a human, on purpose.

### Skipped entirely when there is nothing to do

If `mediatools` already finds ffmpeg — bundled by a distribution, installed by a
package manager, dropped in by hand, or present from a previous install sharing
the data directory — nothing is fetched and nothing is said. The common Linux
and macOS cases therefore never see this at all, which matches 0043's "Windows
first" scoping.

### A setting that survives, and an environment variable that pre-empts

`media_tools_auto_install`: `true` | `false`, settable before first run through
the same configuration the data directory and listen address already use, so an
unattended or scripted install can refuse it without a screen. An air-gapped
deployment sets it once and is never asked again.

### Everything security-relevant from 0043 is unchanged

Pinned URL per platform. Checksum verified before anything is unpacked. No
caller-supplied URL. Atomic move into place. A version bump stays a code change.
The fetch is **not** admin-authenticated, because at first run there are no
accounts yet — which is precisely why it must be *declinable on the screen that
announces it*, since the usual gate does not exist at that moment.

## The README changes, and that is the real cost

The principle cannot stand unedited and honest. It would become:

> **No phone-home.** Local-first, LAN-first. Your library, your viewing and your
> accounts never leave the machine, and nothing is required to reach the
> internet for the server to work. The one exception is stated on the setup
> screen and can be declined there: on first run the server offers to fetch
> ffmpeg, a pinned third-party build, because without it most libraries cannot
> play. Remote access is opt-in and self-owned — never a relay you rent.

That is a weaker sentence than the one it replaces, and the weakening is the
price. Anyone who chose LANcast partly for that sentence is entitled to notice
that it now has an exception in it, and the exception should be *in the sentence*
rather than in a footnote somewhere they will not read.

An exception that has to be written into the principle is the right kind of
exception. One that does not is how principles become decorative.

## Alternatives considered

**Prompt on first run, and wait for an answer.** The same screen, but the fetch
does not start until somebody chooses. Costs one click and preserves the
principle's third job intact — nothing outbound without an affirmative act.
Rejected only because it is materially the same experience for the attentive
user and worse for the inattentive one, who is exactly the user this exists for.
**This is the closest alternative and the one to fall back to if the amendment
above is judged too expensive**, because it fixes the discovery problem without
touching README at all.

**Offer it at the point of failure.** Keep the Settings row, and surface the same
action wherever the absence bites — the player's error, the Live TV screen, empty
durations. No unrequested traffic ever, and no README change. Rejected as
*insufficient alone*: it is strictly better than today and should probably be
built regardless, but it still arrives after somebody has already formed the
impression that the software cannot play their files, which is the impression
0043 was written to prevent and did not.

**Bundle it in the installer.** No network at all, works offline, no principle to
amend. Rejected by 0043 on three counts that all still hold, and one that got
worse: the measured payload is 160MB compressed against a 17MB installer, so the
"multiplies the download for the majority who direct-play" argument is stronger
now than when it was made. It also makes LANcast a redistributor of GPL
binaries, and does nothing for anyone already installed.

**Keep 0043 as it stands.** The status quo is a documented, deliberate decision
rather than an oversight, and it has one genuine advantage: the principle stays
absolute, and an absolute principle is easier to keep than one with an exception
in it. Rejected because the failure it permits is not a missing feature but a
**wrong conclusion about the software**, arrived at by the one user who reported
it and presumably by others who did not.

## Consequences

**Most installs stop being broken in a way nobody can see.** That is the whole
point and it should be stated first.

**The principle acquires an exception, permanently.** Not a temporary one, and
not one that gets quietly narrower later. Every future "it would be so much
easier if the server just fetched X" now has a precedent to point at, and the
answer to those will have to be argued rather than looked up. This is the cost
that is easy to underrate today.

**First run gets slower and can fail there.** A 160MB download at account
creation is a visible operation with progress and cancellation (0043 already
requires this), and it can fail on a slow or filtered connection at the least
welcome moment. A failure must leave a *usable* server and a *clear* Settings
row, never a stuck setup.

**An air-gapped install needs one more thing set.** The environment variable, or
a decline on the screen. Both are one action, and the manual file-drop route from
0043 is unchanged.

**Nothing changes for a server that already has ffmpeg**, which is most Linux
installs and every machine where somebody solved this by hand.

## Open questions

**Does the setup screen block on the download, or proceed alongside it?**
Proceeding is friendlier and means the server is usable immediately; blocking
means the first playback cannot fail for want of tools. This is a real UX
decision and is not settled here.

**What does a member see on a server whose admin declined?** Today they see
files that will not play. That is not made worse by this ADR, but it is not
fixed by it either, and the point-of-failure alternative above is what would fix
it — which is an argument for building that as well rather than instead.

## Revisit when

Someone reports the first-run download as unwelcome — that is the signal that
the exception was not worth it, and it should be believed the first time rather
than argued with. Or if bundling becomes reasonable, which would be a smaller
ffmpeg or a genuine offline-install requirement, at which point this ADR and the
0043 alternative it inherits are both superseded.
