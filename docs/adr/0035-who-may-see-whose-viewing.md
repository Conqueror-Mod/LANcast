# ADR 0035 — Who may see whose viewing

Date: 2026-08-15 · Status: accepted

> **Amended by [ADR 0045](0045-live-presence-between-paired-servers.md)**, which
> adds live presence across a paired server. Two halves, and only one of them
> was invited: sharing with *a named person rather than everybody* is the
> granularity parked below under "What this does not decide" and named first
> under "Revisit when", now asked for. Disclosing what somebody is watching
> **right now** is not — it is stronger than the resume position excluded by
> name below, and 0045 says plainly that this ADR would not have permitted it.
> The distinction 0045 rests on is drawn from the reasoning here: the harm named
> below is a record that *accumulates*, and presence is never written down.

## Context

Three features have now been built around a question nobody had answered, and
each of them recorded the deferral rather than deciding:

- **Trending** counts *accounts*, not plays, and reports how many accounts
  contributed so a client can avoid calling one person's history a trend. It
  names titles and never names people — because naming people was the part
  nobody had decided.
- **Ratings** were built as the private half only: your rating is yours, the
  routes carry no user id, and there is no household average. The roadmap entry
  says so explicitly.
- **Find Friends** and **viewer stats** are still in the backlog with the note
  *"two of them need a decision about who may see whose viewing that nobody has
  made"*.

That is four deferrals across three passes. Deferring a decision while building
around it has a cost that compounds: each feature encodes an assumption about
the answer, and the assumptions are not obviously compatible. Trending already
publishes an aggregate over everybody's viewing without asking anyone; ratings
publish nothing. Both were defensible in isolation and they are not the same
policy.

The question has to be answered before the fifth feature is built on top of it.

### What makes this different from an ordinary permission

Most access rules in this system protect the *server*: filesystem browsing,
library creation, plugin installation, account management. They are admin-gated
because they are operational powers, and ADR 0015 settled that.

Viewing history is not operational. An administrator legitimately controls what
the server does; that does not extend to what the people using it watched. The
household case makes this concrete — the person who set the server up is
somebody's parent, partner or flatmate, and "admin" is a statement about who
manages the disk, not about who may read whose evening.

There is also an asymmetry worth naming: **a media server knows something about
people that almost nothing else does.** What somebody watches, at what hour,
and what they abandoned halfway through is a more intimate record than most
services hold, and it accumulates silently. That is an argument for a
conservative default rather than a convenient one.

## Decision

**Viewing is private by default, and shared only by an explicit per-account
opt-in.**

1. **Default private.** A new account shares nothing. Nobody — including an
   administrator — can read another account's history, resume positions,
   ratings or reviews through the API.

2. **The opt-in is per account and self-service.** Each person decides for
   themselves, in their own settings. An administrator cannot set it on
   somebody's behalf; a switch an admin can flip is not consent.

3. **What sharing means is bounded and stated.** Opting in shares *what you have
   watched and finished* — titles, and when. It does **not** share:
   - **ratings and reviews**, which stay private unconditionally. A note written
     for yourself is not activity, and the wording that invites honesty ("nobody
     else can see this") has already shipped;
   - **resume positions**, which say where you stopped rather than what you
     watched, and are useful to nobody else;
   - anything from a library the reader cannot themselves see.

4. **Aggregates that name nobody need no opt-in.** Trending stays as built: it
   reports how many *accounts* played something without saying which, and a
   count of two or more is not a fact about a person. This is the existing
   behaviour, now with a rule behind it rather than an omission.

5. **Opting out is retroactive.** Turning sharing off hides past activity as
   well as future — the alternative is a switch that cannot take back what it
   gave, which is not a switch.

## Consequences

**Good — the two blocked backlog items become buildable.** A people page can
show who else is on the server, and show activity only for those who chose to
publish it. Viewer stats become "what the people who opted in have been
watching" rather than a surveillance feature nobody asked for.

**Good — the default is the safe one.** A server upgraded into this ADR shares
nothing until somebody deliberately turns it on. No existing history becomes
visible as a side effect of an update, which is the failure mode that would be
unrecoverable — you cannot un-show a history.

**Good — it composes with what is already built.** Ratings need no change.
Trending needs no change. The rule was chosen to match the two conservative
implementations rather than to force a rewrite of either.

**Cost — a household that wants to share must each say so.** Four people means
four switches, and nobody will find them without being told. Mitigated by the
people page saying plainly that others may have chosen not to share, so an
empty list reads as a choice rather than a fault — but it is a real cost of
choosing consent over convenience, and it is the right way round.

**Cost — an admin cannot audit viewing.** Deliberate. The audit log records
administrative acts (ADR 0026) and explicitly excludes browsing and playback,
for the reason given there: burying deliberate acts under a million routine ones
makes a log unreadable. This ADR extends that to the API: there is no route that
answers "what has this person been watching", and adding one later would be a
reversal of this decision rather than an addition to it.

**Cost — one more column and one more thing to explain.** `user.share_activity`,
defaulting off.

## What this does not decide

**Whether sharing should ever be granular** — per library, per media type, or to
named people rather than everybody on the server. A household of four does not
need an access-control matrix, and the switch can gain shape later without any
of the above being wrong. Starting granular would be designing for a scale this
project does not have.

**Whether an account may be deleted along with its history.** Account deletion
exists; what happens to `playback_state` on the way out is a data-retention
question this ADR does not touch.

## Revisit when

Somebody asks to share with one person rather than everybody, a server grows
past a household, or the opt-in proves to be a switch nobody ever finds — in
which case the honest response is to ask why it is worth having, not to flip
the default.
