# ADR 0055 — An intro is what every episode shares

Date: 2026-09-02 · Status: **proposed**

Stage 2 of [ADR 0054](0054-a-marker-says-where-the-film-stops.md), which
recorded intros as a second stage and deliberately did not design them. It
reuses 0054's `item_marker` table and its worker shape; `kind = 'intro'` has
been permitted since revision 37 precisely so this needs no migration.

## Why credits detection says nothing about intros

The credits detector looks for a black frame in a window near the end. Nothing
about that transfers. An intro is not visually distinct — it is a title
sequence, often over motion, and the frame before it looks like the frame
after.

What makes an intro findable is that **every episode of a season carries the
same one**. That is a fact about a set of files rather than about any one file,
which is also why the technique says nothing about a film: there is nothing to
compare a film with.

## The measurement

Audio is fingerprinted into one hash per 100ms frame — bits describing the
*shape* of the spectrum rather than the audio itself, so two encodes at
different volumes still agree. Episodes are compared pairwise and the longest
agreeing stretch is the candidate.

Run over four shows in a real library, five episodes each:

| show | episodes answering | run length | position |
|---|---|---|---|
| It's Always Sunny S3 | 5/5, all at 4/4 agreement | **30.2, 27.1, 30.2, 30.1, 30.1s** | 55, 113, 44, 119, **193s** |
| Black Books S1 | 5/5 at 4/4 | 17–27s | 0.5–50s |
| Cowboy Bebop S1 | 5/5 | 5.8–48.9s | 3.6–50.6s |
| Futurama S2 | 4/5 | 11–27s | 20–103s |

**Sunny is the result worth reading.** Five episodes agree on a ~30 second
block to within a second, at five positions spanning two and a half minutes.
That is a fixed title sequence behind a cold open of variable length, which is
what the show actually has — and it is the direct evidence for the rule below
that **no marker may assume a fixed timestamp**. A detector that averaged
positions would put Sunny's intro at 105s, which is inside an episode in every
one of the five.

The weaker rows are honest about where this is least reliable. Cowboy Bebop's
theme is long and musical and returns runs from 5.8 to 48.9 seconds; Futurama
answers on four episodes of five. Neither is failure, but neither is a marker
anybody should skip to yet.

## What nearly buried it, and is worth recording

The first run over real episodes found almost nothing: inconsistent offsets, no
agreement. The obvious conclusion was that the technique does not work.

It was wrong, and the way it was wrong is the useful part. Matching **a file
against itself**, decoded twice, gave 0.00 bits of 16 differing. Shifting the
second decode by one whole frame hop gave 0.03. Shifting it by **half** a hop
gave **3.08 bits — on identical audio**.

A frame hop is 100ms and two episodes have no reason to begin their intro on
the same 100ms grid. That noise floor put real episodes at 4.2 bits against
7.9 for random alignment: a signal, but far too weak to align on.

The fix is to search the phase rather than hope for it — one side is
fingerprinted at four sub-hop offsets and the best is kept. In a test that pins
this, one phase finds 0.9 seconds of a known 20-second match and the sweep
finds 20.2.

**The lesson is the same one this project keeps relearning.** A component that
appears not to work should be tested against itself before it is redesigned.
Two rounds of changing the hashing were spent before that check was run, and
neither helped, because neither was the problem.

## Decision

**An intro marker is written only where several episodes of the same season
agree, and it is stored per episode rather than per show.**

**1. The season is the unit of comparison.** Not the show: intros change
between seasons, and a rule that compared across a whole run would find the
weakest thing common to all of them or nothing at all.

**2. Agreement is required, and by length rather than position.** The
measurement says the length of the shared block is stable to within a second
while its position moves by minutes. A candidate is accepted when a majority of
compared pairs return runs of consistent length; its position is then taken
from that episode's own match and never from an average.

**3. Both ends are stored.** `start_ms` and `end_ms`, because unlike credits an
intro has a real finish — the point a viewer would skip *to*. This is what
`end_ms` was made nullable for in revision 37.

**4. It stores evidence and skips nothing**, exactly as stage 1 does. No client
draws a skip control from this, and for the same reason: nobody has watched an
episode and confirmed a single one of these timestamps. Sunny's consistency is
detectors agreeing with each other, which is not the same as being right.

**5. Comparison is bounded.** Only the first seven minutes of each episode are
fingerprinted, and an episode is compared against at most a handful of others
from its season rather than all of them — pairwise over a 26-episode season is
325 comparisons to learn what four would say.

## Consequences

**This is more expensive than credits detection**, and unlike it the cost is
not per file but per season: an episode cannot be examined alone. A season is
therefore the unit of work, and a season with one episode is skipped rather
than being a failure.

**The API gains nothing new.** `GET /api/items/{id}/markers` already returns a
list and already documents `intro` as a possible kind. That was the point of
writing it into the contract before anything produced one.

**`internal/marker/introlab` is kept** as the instrument. The tolerance, the
minimum run and the head window are tuning constants, and changing one is a
claim about real television that should be checked against real television. It
is a `main` package under `internal/`, so `go build ./...` compiles it and
goreleaser never ships it.

**Shows whose episodes have no shared audio produce nothing**, which is a real
answer. So do shows with one episode per season, and shows where the rip
differs between episodes enough to defeat the fingerprint.

## Alternatives rejected

**Assume a fixed timestamp per show**, entered by hand or averaged. Rejected by
the measurement rather than on principle: Sunny's intro sits between 44s and
193s across five consecutive episodes, and any single number is wrong for four
of them. The failure mode is skipping into the middle of a scene, which is
worse than offering nothing.

**Compare video rather than audio.** A title sequence is visually similar
frame-to-frame, and the credits work already showed how little a black frame
distinguishes. Audio is both cheaper to decode at 8 kHz mono and far more
distinctive.

**Use a third-party fingerprinting library.** Chromaprint is the obvious
candidate and solves a harder problem than this one — identifying a recording
against a global database, rather than finding what two known files share. The
whole fingerprinter here is under 200 lines with no dependency, and CLAUDE.md's
rule about not adding a third-party player library on the strength of an
existing one applies in spirit: this build vendors what it can justify.
