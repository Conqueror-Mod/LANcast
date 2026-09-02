# ADR 0054 — A marker says where the film stops

Date: 2026-09-01 · Status: **proposed**

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
