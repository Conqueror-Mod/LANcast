# ADR 0054 — A marker says where the film stops

Date: 2026-09-01 · Status: **accepted** 2026-10-04, built and amended (see the amendments below)

Stage 1 of two. This one is about **credits**, which both films and episodes
have. Intros are the harder half, they exist only on episodes, and they need a
different engine — they are recorded at the end here and not decided.

## The report, and what was actually wrong with it

Reported as: *"a movie has to run completely through to the last second to be
counted complete, and we have no way to skip an intro."*

The first half is not what the code does. `WatchedThreshold` is 90 on the live
server, applied server-side on every progress write
([settings.go](../../internal/config/settings.go)), and the client marks done
at 92% of its own duration. The live database agrees — *Tokyo Drift* at 93.9%
and *Beetlejuice Beetlejuice* at 93.8% are both `watched = 1`, and the only
unwatched row above 80% sits at 80.2%.

**But the reporter was right that something stopped titles completing, and it
was the denominator.** `duration_ms` had two writers: ffprobe, and TMDB's
`runtime` in whole minutes. The provider won. Across a 40-file sample of the
live library, one in eight disagreed with the file by more than 2%:

| Title | `duration_ms` | file | error |
|---|---|---|---|
| Ghostbusters | 117.0 min | 133.7 min | −12.5% |
| Jackass Presents: Bad Grandpa | 92.0 min | 102.5 min | −10.2% |
| The Good, the Bad and the Ugly | 161.0 min | 178.8 min | −9.9% |
| The Book of Eli | 118.0 min | 110.2 min | **+7.0%** |
| A Charlie Brown Christmas | 25.0 min | 25.7 min | −2.8% |

Every declared value is a whole number of minutes, which is what gives the
source away. The last column is the one that hurts: when the runtime
*overstates* the file, 90% of it lands past the end, and the title cannot be
finished by watching it. That is the reported symptom exactly.

**Fixed before this ADR was written**, because it is not a design question —
a duration is measured, not described, and a source may now supply one only
when nothing has measured it. Existing rows are repaired by re-probing.

It is recorded here because it is the reason this ADR cannot express a marker
as a percentage. A percentage of a wrong duration is how we got here.

## What is left, and why the cheap answer does not work

What remains is real: there is no way to *jump* to the end of the credits or
past an intro, and 90% is a guess about where the credits start rather than a
fact about this film.

The obvious first move is to read container chapters in the probe worker. It
is nearly free and the parse side is already split from the process side. It
was measured on 25 films and 25 episodes of the live library before being
designed around:

| | has chapters | titles usable |
|---|---|---|
| Movies (1,200) | 9/25 | **none** |
| Episodes (992) | 10/25 | **none** |

Only ~38% carry chapters at all, and not one title names anything. They are
raw timestamps (`00:09:25.190`) or ordinals (`Chapter 01`). A container chapter
says where an encoder put a seek point, not what happens there.

**So chapters are rejected as a source.** They cannot answer the question even
in the minority of files that have them.

## Decision

**A marker is a measured timestamp stored against an item, produced by its own
worker, and the API serves it as a fact the client draws a button from.**

Four parts, and the ordering is the point.

**1. Markers are their own table, not columns.** `item_marker(item_id, kind,
start_ms, end_ms, source, confidence, created_at)`, with `kind` in
`credits`/`intro` — intro is in the schema from the start so stage 2 does not
need a migration, and nothing writes it yet. A side table because an item may
carry several markers, because a marker has provenance, and because
`media_item` is already 45 columns wide (ADR 0002 chose that shape; it did not
choose it as a place to keep growing).

**2. Detection runs in its own worker**, beside probe and enrich. It is a
second full pass over the file's tail and folding it into probe would make
every scan pay for it, which is the same reasoning that put probing outside
enrichment. Process execution stays split from the decision: the ffmpeg
invocation is one function, and the rule that turns its output into a
timestamp is pure and tested against captured `blackdetect` output with no
media on disk.

**3. A marker is evidence before it is a feature.** The first build stores
markers and exposes them for inspection; it does **not** move the watched
threshold and does **not** draw a button.

That was originally written because the selection rule was unknown —
`blackdetect` over four film tails found candidates in all four and eight in
one of them. Rather than leave it at that, a harness gathered black and silent
stretches from the tails of **40 films**, and scoring six rules against them
eliminated three outright:

| rule | answers | median | too early | past 99.5% |
|---|---|---|---|---|
| earliest ≥5s within 88–99.5%, else ≥2s | **39/40** | **94.1%** | **0** | **0** |
| earliest ≥5s, refusing the last 0.5% | 32/40 | 94.0% | 1 | 0 |
| earliest ≥2s | 40/40 | 93.1% | 3 | 1 |
| longest black run | 40/40 | 95.5% | 1 | 4 |
| earliest ≥5s overlapping silence | 20/40 | 96.3% | — | 7 |
| last ≥5s | 33/40 | 99.7% | 0 | **20** |

**The last black run is the file ending, not the credits starting** — 20 of its
33 answers land in the final half-percent. **Silence is worse than useless
here**: it answers for half the sample and lands late when it does, because
credits have music over them. *Hollow Man* has exactly one silent stretch in
its entire tail. And the **longest** run is usually the final fade-out.

What survives is the *earliest* sustained black run, and its one failure mode
has a name: a deliberate fade-to-black inside the third act. Five films picked
one, and constraining the search to start at 88% moved every one of them to a
plausible position — *The Beastmaster* 77.9% → 93.9%, *Blow* 77.6% → 95.4%,
*Generation X* 84.2% → 97.2%.

**That 88% was tuned on the same 40 films it came from, which is not
evidence — so it was tested on 40 it had never seen**, with the rule and its
constants frozen in code before the second sample was gathered:

| | tuned (sample 1) | **held out (sample 2)** |
|---|---|---|
| answered | 39/40 | **38/40** |
| median boundary | 94.1% | **94.3%** |
| range | 88.3–99.3% | **89.3–99.2%** |
| outside the window | 0 | **0** |
| pressed against the 88% floor | 1 | **0** |

**The floor is not doing the deciding.** That was the specific way this could
have been overfitted, and it is why the validator counts it: if 88% were a
number fitted to the first sample, the second would pile up against it. Nothing
does. The lowest held-out answer is 89.3%, a full point clear, and 26 of the 38
sit between 92% and 96%.

**Both abstentions are principled**, which matters more than the count. *Jackass
2.5* is a clip film whose longest tail black run is 1.8s, under the 2s
fallback. *At World's End* has **one** black run in its entire tail, at 99.9% —
its credits begin on a cut, not a fade. That is the real limit of this method
and no threshold fixes it: a film that does not fade into its credits has
nothing here to detect.

Lowering the fallback to 1.5s would answer *Jackass 2.5*. It is **not** being
lowered, because that number would then have been chosen by looking at the
held-out set, and the honest version of this table would no longer exist.

What all of this proves is that the rule is **consistent**, not that it is
**right**. Nobody has yet watched a film and written down where its credits
begin. That is the only ground truth there is, it is the reason stage 1 exposes
markers for inspection rather than acting on them, and no amount of agreement
between detectors substitutes for it.

Two things are settled regardless. The median boundary sits at **94%**, so the
90% threshold was never a credits estimate and a marker is not a refinement of
it. And *The Outsiders* shows that a wrong duration corrupts the reading
itself: its black frames landed at 120% of what the database claimed.

**4. Once a rule is proven, the marker replaces the guess.** A credits marker
becomes the watched threshold for that item — finishing means reaching the
credits, not 90% of a runtime — and the percentage stays as the fallback for
every item with no marker, which will always be most of them. The setting is
not removed.

## Consequences

**The API gains a contract**, so `docs/api.md` changes in the same commit.
Markers ride on the item payload rather than needing a fetch: a client that
must ask a second question before it can draw a button will draw it late, and
a button that appears three seconds into the credits is worse than none.

**Detection is expensive and opt-out.** It reads a quarter of every file. It is
paced like enrichment, it never runs during a scan, and a library can decline
it — the same shape as NFO writing, which is off by default for a comparable
reason.

**A marker is not locked and is not user-editable in this stage.** Hand
correction is the obvious next request and it is deliberately absent: the
locked-fields rule means adding it commits us to never overwriting it, and that
promise should be made after the detector is trustworthy rather than as
insurance against it not being.

**Nothing here helps intros**, and the reported half about intros stays open.
The engine for those is cross-episode audio correlation within a season — an
intro is findable precisely because every episode shares it, which is also why
the technique says nothing about a film. It is stage 2, it reuses this table
and this worker, and it is not designed here.

## Alternatives rejected

**Container chapters** — measured above. Present on 38% of files and semantically
empty on all of them.

**A fixed offset per series, entered by hand.** Cheap, and it is what several
clients do. Rejected because it is a fact about a *release* rather than a
series: a season with a shortened intro, a pilot with none, and a recap before
the titles all break it silently, and the failure mode is skipping into the
middle of a scene, which is worse than not offering to skip.

**Detection at first play.** No background cost, but the answer arrives after
the viewer needed it. For credits specifically it is self-defeating: the first
viewer, the one who most wants the marker, is the one guaranteed not to have it.

**Asking a metadata provider.** No provider serves this, and if one did it
would be a fact about a theatrical cut rather than about the file on disk —
which is the mistake this ADR opened by fixing.

## Amendment — 2026-09-11: the stamp belongs to the pass that owns the kind

Stage 2 arrived and quietly switched this one off for every episode.

`SaveMarkers` takes the kinds a pass is authoritative about, so that the credits
detector cannot delete an intro marker by writing an empty list. It then stamped
`markers_at` — **this** pass's "looked at this file" flag, the one
`PendingMarkers` selects on — regardless of those kinds. The intro pass writes
one marker per episode through the same method, so every episode it examined was
recorded as examined for credits without a frame being decoded.

Measured on a real library the moment it was suspected:

| | stamped | with a credits marker |
| --- | --- | --- |
| episodes | 994 of 994 | **0** |
| films | 1,208 | 1,112 |

Films are the control: no intro pass touches one. The failure was invisible
because "stamped with no marker" is also what an honest abstention looks like —
a file whose credits begin on a cut produces nothing either — so the log, the
counts and the API all read exactly as they would on a library of unusual films.

**`markers_at` is stamped only when `credits` is among the kinds.** An intro
write leaves the flag alone, in both directions: it does not set it, and it does
not clear one the credits pass has earned, because the two passes run in either
order.

**Revision 46 clears the stamp on any episode that carries one without a credits
marker.** An episode that really was examined kept its marker and keeps its
stamp; one that cannot be told apart from an abstention is looked at once more.
That costs one wasted decode per genuine abstention, against a library that
would otherwise never carry a credits marker on an episode at all.

The lesson is the one ADR 0055 records about `intros_at` from the other side: a
shared write path needs to know whose flag it is setting, and a flag that two
passes can set is a flag neither of them owns.

## Amendment — 2026-10-03: somebody looked, and the rule needed a gate

Decision 3 said the rule was consistent and not known to be right, and that
nothing would act on it until somebody checked. This is the check.

**Method.** For every film in both samples, frames were pulled at −60, −30, 0,
+30, +60 and +120 seconds around the stored marker, and each was judged by eye:
*early* (the film is still running after the marker), *right*, or *late* (the
credits were already rolling before it).

**The first sample, 40 films, today's rule:** 28 right, 3 late, and **9 early**
— *Green Street Hooligans*, *Space Jam: A New Legacy*, *Captain America: The
Winter Soldier*, *Twister*, *Star Trek V*, *Resident Evil: Degeneration*,
*Pollyanna*, *Fantasia* and *Alien³*. Every early one is a fade to black inside
the film with a scene after it, and every one but *Alien³* sits at or below 92%.
One time in five, a **Skip credits** button would have dropped somebody out of
the third act. That is worse than no button, and it is why `web/src/lib/skip.ts`
offered intros only.

**What a black run cannot say, the frames after it can.** Credits are mostly
black *and* sharp: lines of text on a dark ground. Two numbers per frame
separate them — the share of near-black pixels (`blackframe`, luma under 32 of
255) and the mean of an edge map (`edgedetect`, at 480 pixels wide). A scene
after a fade is rarely more than 60% black; credits are 80–100%. *Fantasia* is
the reason for the second number: its fade goes to a dark, empty concert stage,
black enough to pass and with nothing written on it.

**The gate.** Candidates are tried in the old order, and one is accepted only if,
of the frames at +15/+30/+45/+60/+90 s, at least **4** are **≥80%** near-black
and at least **2** have edge density **≥2.0**. Fewer frames fit near the end of
the file, and the bar scales with them. A frame that cannot be read counts
against. If no candidate passes, the film has no marker — falling back to the
earliest run would return exactly the answer the gate exists to doubt.

**Tuned on sample 1, then frozen** — the simulation script was hashed before it
was first run on the second sample — **and run on sample 2**, 40 films neither
rule had been fitted to. Every answer it gave, and today's answer wherever the
two differed, was judged by eye:

| | sample 1, today | sample 1, gated | **sample 2, today** | **sample 2, gated** |
|---|---|---|---|---|
| early | 9 | 1 | **5** | **0** |
| abstained | 0 | 7 | 0 | **9** |
| answered | 40 | 33 | 40 | **31** |

Across all 80: **early answers fall from 14 to 1.** The one left is *Resident
Evil: Degeneration*, a dark computer-animated film whose final scene is, to
these two numbers, text on black.

**What it costs**, from the held-out sample, because that is the honest count.
Four films the old rule had right now get no answer: credits drawn as comic
panels (*Batman: Year One*), credits too dim to register (*Terminator: Dark
Fate*), a crawl that follows a mid-credits scene (*Thor: Ragnarok*), and a short whose end card is
mostly logo. *The Dark Knight Rises* gets worse rather than silent: its credits
open with a long run of almost empty black frames, the gate turns them away, and
the answer lands on the copyright card at 99.5%. Late, so harmless, and useless.

Abstaining is the cheap failure — a film with no marker shows no button — and
the asymmetry between that and an early skip is what the thresholds are set by.

**About ten films are late under both rules**, and it is one pattern: credits
that open with styled titles over the final scenes, after which the black run
arrives. The skip lands inside the credits rather than at their start; nobody
misses any of the film. Moving those earlier is a different problem and is not
attempted here.

**The source changes, and that is the safety catch.** Gated markers are written
as `blackdetect-gated`. A marker from the old rule stays in the table until the
re-run reaches it, and the player offers a skip only from the new source — so a
library part-way through re-detection never offers an ungated answer.

**Revision 54 re-queues every item carrying a `blackdetect` credits marker.**
Abstentions are left alone: the gate only removes candidates, so a file with
none abstains again. The re-run costs one tail decode per film plus five
single-frame reads per candidate — 80 films' frame reads took 50 seconds.

**A new length un-stamps the credits pass.** Four markers in a real library sat
outside the window of their own file — *Alien³* at 73.2%, *1408* at 81.5%,
*AVP* at 85.7%, *13 Ghosts* at 99.9% — each chosen against a length the file did
not have, and never revisited when a probe recorded the real one. `SaveProbe`
now clears `markers_at` when the duration moves by more than a second. All four
carry the old source, so revision 54 reaches them too.

**Decision 3 is lifted for credits, narrowly.** A **Skip credits** button may be
drawn from a gated marker: a visible button the viewer presses, never an
automatic jump, offered from the marker to the end and not in the final 1% of the
file, where there is nothing left to skip. Decision 4 — the marker replacing the
watched threshold — is **not** taken by this amendment. A late marker is
harmless to a button and would not be harmless to "finished".

## Amendment — 2026-10-03, later: episodes are not gated

The gate was tuned and tested on films. The same check was then run on 40
episodes drawn from every series in the library, two per series and the rest at
random, with every marker judged by eye from frames around it:

| 40 episodes | count |
|---|---|
| ungated marker right | 31 of 33 |
| early | **1** — a Voyager scene ending on a warp-out to black |
| late | 1 (harmless) |
| no candidate at all | 7 |

Television fades into its credits far more reliably than film does. The gate
accepted 26 and **discarded six right answers** to save the one Voyager
episode: closing credits drawn over artwork (*Cowboy Bebop*, *School Days*),
credits on dark blue and gone in under thirty seconds (*Futurama*), credits
running straight into a bright network card (*Blue Mountain State*). On
television the frame test costs more than it saves.

**So an episode is examined without the gate**, and its marker keeps the
ungated source, `blackdetect`. The player offers that source on an episode and
not on a film, where the same rule was early one time in five. Revision 55 puts
the stamp back on every episode revision 54 had re-queued with an ungated
marker, since re-examining it would decode its tail to reach the marker it
already has.

This is an interim rule. What episodes in a season really share is their
closing music, which is findable even when the credits start on a cut with no
fade — the seven episodes with no candidate at all. Matching it across a season,
with the intro detector's fingerprint engine, is the next step.

## Amendment — 2026-10-04: an episode's credits come from what its season shares

The interim rule above left two gaps: an episode whose credits begin on a cut
has no black run to find (Black Books, Death Parade, a School Days and a League
episode — 7 of 40), and the black run's one failure, a fade inside the last act,
was still there for episodes too.

What a season's episodes share at the end is their **closing theme**. The intro
detector's fingerprint engine (ADR 0055) finds what episodes share at the
start; pointed at the last five minutes, it finds where the closing music
begins, and a scene fade cannot fool it, because a scene is not shared across a
season. Run over every season in the library — 934 of 1,074 episodes found —
it agreed with the eyeballed credits to within seconds on Star Trek, Futurama,
The League and Cowboy Bebop.

It fails in two ways of its own. Where the closing music is not shared, what
the season shares is the channel ident at the very end — HBO's on Silicon
Valley, the network card on Sunny and Blue Mountain State — always within
seconds of the end: measured, idents sit 0–19 s from the end and closing themes
30 s and beyond. And where the theme starts over the final shot it lands a few
seconds early.

**So the two signals check each other** (`marker.EpisodeCreditsFrom`):

| evidence | answer |
|---|---|
| shared audio under 20 s from the end | ignored — an ident |
| both, within 20 s of each other | the black run, which is exact to the frame |
| black run more than 20 s before the music | the black run if the frame gate passes it (text on black), else the music (a fade) |
| music more than 20 s before the black run | the music (a closing song whose end the black run found) |
| only one | that one |

The frame gate, wrong for television as a rule, is right as a tie-breaker: it
is asked only whether the one black run that disagrees with the music is text
on black or a scene.

**Tuned on episodes from every series, frozen (script hashed), then run on 40
episodes nobody had looked at**, every answer judged by eye from frames around
it:

| 40 held-out episodes | black run alone | **combined** |
|---|---|---|
| answered | 36 | **40** |
| early | 3 | **0** |
| late (harmless) | 2 | 2 |

The three early answers were all fades the music overruled — a title card in
*It's Always Sunny* ("Hour 48"), a dark scene in TNG, a scene before a toast in
Voyager. The two late ones are TNG's first season, where the credits begin over
the final shot and neither signal sees them.

**The season pass now decides episode credits**, alongside intros: it decodes
each episode's last five minutes of audio, scans the last 13% for black, and
writes credits and intro in one call. Writing credits stamps `markers_at`, so
the per-file pass leaves the episode alone; a one-episode season never reaches
the season pass and keeps the per-file answer. Markers placed by the music carry
the source `ending-audio`, offered on episodes only. **Revision 56** clears
`intros_at` so every season is compared again, and a new length now clears it
too, since a new cut moves the closing theme.

Still not detected by the music: *School Days*, 0 of 12 — the longest stretch
its episodes share at the end is a few seconds, and why has not been looked
into — and *Storm of the Century*, a three-part miniseries. Both keep whatever
the black run finds.

