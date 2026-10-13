# ADR 0083 — An intro skips itself

Date: 2026-10-12 · Status: **accepted** 2026-10-12 · amends
[ADR 0054](0054-a-marker-says-where-the-film-stops.md) decision 3, for intros only

The plan is [settings-plan.md](../settings-plan.md), step 5. Chris confirmed the
default on 2026-10-12 (§6, decision 5): **intro skip happens automatically by
default**, and an account can turn it off.

## Context

ADR 0054 decided that a marker is evidence before it is a feature, and the
player has kept to that ever since: **Skip intro** and **Skip credits** are
buttons that appear, never jumps that happen. Its stated reason, repeated beside
the button in `Player.tsx`, was that an automatic skip a few seconds wrong
cannot be told apart from a broken file, and the first thing it would eat is a
cold open.

Since then the intro detector has been measured and corrected (the 0.5 s bridge
and the title-card rule, v0.9.16–v0.9.17), and has marked 911 of 994 episodes in
the library. Every intro marker checked by eye landed inside a title sequence.
Pressing the same button at the same point of every episode of a season is now
the commonest thing anybody does in the player, and a person who wants the
theme can still have it.

## Decision

**When playback reaches an intro the player would offer to skip, it skips it
and says so, with one press to go back.** On by default, per account.

1. **The rule that draws the button is the rule that skips.** The automatic
   skip uses `skipTarget` in `web/src/lib/skip.ts`, unchanged: an `intro`
   marker with an end, while the playhead is inside it. No new trust rule, and
   no marker the button would refuse.
2. **It says what it did.** In place of the button, a notice reads *Intro
   skipped*, with **Watch intro**, for eight seconds. That is the answer to
   0054's objection: a jump with a label next to it is not mistaken for a
   broken file, and a wrong one costs a single press.
3. **Watch intro is final for that episode.** It seeks back to the start of
   the intro and turns the automatic skip off for the item until it is played
   again, so the theme plays through and the ordinary button stays offered.
4. **Once per playback, and only going forwards.** The skip happens the first
   time playback is inside the intro: on reaching it, or on starting there.
   Seeking back into an intro after that offers the button and does not jump,
   because somebody who scrubbed back to it has said they want it.
5. **Per account, on unless switched off.** `user.skip_intros`, a nullable
   column. Null means the default, so an account that never touched the switch
   follows it. It is served at `GET`/`PUT /api/profile/playback` and shown in
   This device › Player, beside the account's subtitle style, which follows
   the same person to every device.
6. **Credits keep the button.** Skipping credits automatically would end films
   early, and on an episode the Up Next card already does that job when the
   viewer lets it.

## What is not settled

0054 noted that a detected intro end is occasionally a few seconds **early**,
so a skip lands in the last bar of the theme. That is harmless. A **late** end
would clip an episode's first line, and as a jump the viewer did not ask for,
it costs more than it did as a button. No late end has been seen, but no sample
has been taken to look for one. The first weeks of use are that sample. A wrong
skip now has a visible notice and a one-press undo, and a pattern of them is
the evidence for a measurement.

Known limit: a network ident shared by every episode at 0:00 (the Lanterns S1
case, 8.7 s) is marked as an intro and will be skipped. That costs a logo.

## Consequences

- Schema 73 adds a nullable column, `user.skip_intros`.
- New routes: `GET` and `PUT /api/profile/playback`.
- The comment beside the skip button in `Player.tsx` is rewritten. It argued
  against exactly this, and it would otherwise go on arguing against code that
  no longer does what it describes.
- An account that turns it off gets the 0054 behaviour exactly.

## Alternatives rejected

**Off by default, as an opt-in.** That was the plan's first proposal. Chris
chose the other way: the button is pressed at nearly every intro, and a
default should be what most people do.

**Per device rather than per account.** A person who wants the theme wants it
on the TV and the phone alike, which is why their subtitle style moved to the
account too.

**A countdown before the jump**, as some clients do. That is a button with a
timer, and it puts a moving control over the opening seconds of the theme. A
notice after the fact, with undo, interrupts less and costs the same press.
