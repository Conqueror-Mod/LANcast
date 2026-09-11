# ADR 0055 — An intro is what every episode shares

Date: 2026-09-02 · Status: **proposed**

Stage 2 of [ADR 0054](0054-a-marker-says-where-the-film-stops.md), which
recorded intros as a second stage and deliberately did not design them. It
reuses 0054's `item_marker` table and its worker shape; `kind = 'intro'` has
been permitted since revision 37 precisely so no marker table change is needed.

It does add **revision 38**, a single `intros_at` stamp on `media_item`. That
is not the table 0054 anticipated, and it is separate from `markers_at` rather
than shared with it because the two passes are different shapes: credits are
decided per file, an intro per *season*. An episode may be examined for credits
long before its season has enough siblings to compare it against, and one
column could not say so without letting either pass claim the other had run.

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

## What it produces on a real library

Run end to end against a copy of a real database — migration, queue, decode,
fingerprint, rule and stored markers — the pass answers on roughly half the
episodes it examines and refuses the rest:

```
Black Books S1   5 of 6    ~0.5s → ~27s      lengths 24–28s
Futurama S2      8 of 20   ~0.8s → ~29s      lengths 13–28s
Futurama S3      8 of 15   ~0.7s → ~29s      lengths 21–28s
Blue Mountain State S1/S2  0 of 26
```

Both shapes in that table are the design working. The positions cluster near
zero where a show opens on its titles and jump where it does not — Black Books
*Fever* at 62.1s, Futurama *Insane in the Mainframe* at 10.7s — which is the
variable cold open that made a fixed timestamp impossible.

The refusals are the majority rule being conservative: three of four
comparisons must agree, and an episode where only two do is left unmarked
rather than guessed at. Blue Mountain State produces nothing at all across 26
episodes, which is either a show whose episodes share too little audio or a
limit of the fingerprint on that material; it has not been established which,
and the honest record is that it is not known.

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

**2. Agreement is required, and by where the candidates begin.** A majority of
an episode's comparisons must return runs starting within a few seconds of each
other; the marker is then the median start and median end of that group.

This was written the other way round first — agreement by *length* — and the
implementation disproved it. Reading the raw candidates rather than their
summary: Black Books S1E01 matched its four siblings at lengths **23.4, 15.1,
24.9 and 28.2** seconds, a thirteen-second spread, while starting at **4, 3, 2
and 0**. A length is the difference of two noisy quantities and carries both
errors; a start carries one. Clustering on length refused four episodes in six
of that season, and clustering on start finds them.

The first rule came from conflating two facts. Sunny's positions vary between
44s and 193s **across** episodes, which is why no marker may assume a fixed
timestamp — but *within* one episode every candidate describes that same
episode's intro, so those starts agree. Both statements are true and only the
first was in evidence when the rule was written.

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

## Amendment — 2026-09-11: a run crosses a noisy frame, and a title card counts

The pass has run on a real library long enough to judge. It marked **261 of
994** episodes in seasons of three or more. **19 seasons had
nothing at all**, most of them shows with an obvious title sequence.

### What was wrong

`introlab` now runs the shipping peers and rule beside variants of the run
measurement, and reports every candidate. Across the seasons that had nothing,
it found two separate causes.

**A run ended at the first frame over tolerance.** A line of dialogue, a door,
or a sting over the titles broke a thirty-second intro into pieces. Most pieces
fell under the eight-second floor, and the ones that survived started wherever
the first uninterrupted piece happened to begin. The Black Books starts that
decision 2 above attributes to noise (4, 3, 2 and 0) were this. With half a
second bridged, all four start at 0 and run 29–31 seconds.

**A title card is short.** The League's is about four seconds. Every episode of
season 2 matched all four siblings starting on the same second, and nothing
was marked, because eight seconds was the floor.

### Measured, with the shipping comparison

| season | before | 0.5s bridge | 2s bridge |
| --- | --- | --- | --- |
| Blue Mountain State S1 | 0/13 | **13/13** (30s) | 13/13 |
| Cowboy Bebop S1 (12 eps) | 1/12 | **12/12** (90s) | 12/12 |
| Futurama S1 | 0/9 | **9/9** (28s) | 9/9 |
| It's Always Sunny S8 | 0/10 | **9/10** (22s) | 9/10 |
| It's Always Sunny S12 | 0/10 | **9/10** (22s) | 9/10 |
| Star Trek: TNG S6 (12 eps) | 1/12 | **12/12** | 12/12 |
| School Days S1 | 0/12 | **10/12** | 10/12 |
| Black Books S1 | 5/6 | 6/6 | 6/6 |
| It's Always Sunny S3 | 8/8 | 8/8, every start on one second | 8/8 |
| It's Always Sunny S14 | 3/10 | 5/10 | 5/10 |
| The League S2 (card rule) | 0/13 | **13/13** (~4s) | 3/13 |
| The League S5 (card rule) | 0/13 | **12/13** | 1/13 |
| Storm of the Century (3 parts) | 0/3 | 0/3 | 0/3 |

This answers the question above about Blue Mountain State, which was recorded
as unknown. It was the walk, not the material.

### Decision

**A run may cross up to half a second of consecutive disagreement**
(`IntroGapFrames`). The gap is never counted as agreement, and a trailing gap
is not part of the run, so bridging cannot grow a run into what follows it. Two
seconds found nothing half a second missed. It let runs drift past the titles,
and it destroyed the title cards: The League S2 fell from 13/13 to 3/13 as short
cards were bridged into unrelated matches.

**A run of three seconds or more counts as a title card when every comparison
agrees on where it starts, and there are at least three of them.** A majority on
a short run is still what a shared network sting looks like, and is still
refused. A card may not begin in an episode's first two seconds. Silicon Valley
S1 and Lanterns S1, both HBO, each returned a unanimous 5–6 second stretch at
exactly 0.0s, which is the network ident the rips carry. A long intro at 0:00
still goes through the majority rule.

**Revision 45 clears `intros_at` on every episode**, so seasons the old rule
examined are compared again. Without it the fix would reach only episodes added
afterwards. It is a stamp reset and nothing else. Markers already found stay
until the pass replaces them, and intro markers are evidence nobody edits
(decision 4).

### Still not right, and recorded as such

- **An ident longer than eight seconds at 0:00 still passes the majority rule.**
  Lanterns' bridged run is 8.5s, so it stays marked at 0.0–8.7s. Telling an
  ident from an intro that opens an episode needs more than timing.
- **Intros past the head window are cut off.** Two TNG S6 episodes put their
  titles beyond 6:40, and the marker ends at 7:00.
- **Nobody has watched an episode to confirm a single one of these timestamps.**
  Agreement between detectors is still not correctness, and no client draws a
  skip control from them.
