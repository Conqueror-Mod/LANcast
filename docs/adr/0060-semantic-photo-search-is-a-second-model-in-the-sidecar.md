# ADR 0060 — Semantic photo search is a second model in the sidecar

Date: 2026-09-04 · Status: **accepted** 2026-09-04

A picture library is the one library LANcast cannot search. `?q=` matches title
and series, and a photograph's title is its filename — `DSC_0042`, `IMG_2291`,
or a camera's UUID. Everything else has a title somebody meant: a film has one
from a provider, a track has one from its tags. A photograph has a number.

So the picture library has a timeline, folders, and — since #543 — people, and
no way to answer *the one with the red door*.

This records how that gets fixed, and it opens by correcting the answer this
project had already half-adopted.

## It is not a plugin, and the reason is worth writing down

The Immich study ranked semantic search first among the things worth taking and
argued it should be **the feature that proves a plugin SDK** rather than a
server feature. That was wrong, and it was wrong in a way that would only have
surfaced after somebody had built an SDK for it.

**The sandbox cannot run inference.** ADR 0020 grants a plugin exactly what its
manifest asks for out of a fixed set, and the host exports three functions:

```go
NewFunctionBuilder().WithFunc(rt.hostLog).Export("host_log").
NewFunctionBuilder().WithFunc(rt.hostHTTPGet).Export("host_http_get").
NewFunctionBuilder().WithFunc(rt.hostSecret).Export("host_secret").
```

Log, fetch, and read one secret. No filesystem, no process, no native library,
no GPU. That is the whole surface, and it is deny-by-default *by construction*
— "anything not granted does not exist inside the module". A CLIP model is an
ONNX graph executed by a native runtime. There is no version of that which fits
through those three doors.

**The only plugin-shaped semantic search is the one this product refuses.**
`host_http_get` is real network egress to a declared host, so a plugin *could*
implement search — by uploading photographs to somebody else's server and
asking them. That is precisely what the fourth founding principle exists to
prevent, and precisely what face grouping was built on the box to avoid: it is
"the feature Google Photos used to justify reading every family album ever
uploaded" (ADR 0052). Shipping the sandboxed version would be shipping the
capability to become the thing.

So the plugin boundary is not the wrong tool by accident. It is the right tool
for *providers* — fetch a rating, fetch a poster, talk to somebody's API — and
inference is not that.

## The decision

**Semantic search is a second model in `lancast-faces`, not a new component.**

ADR 0052 already made this decision for the identical constraint and paid for
it. Every release target is `CGO_ENABLED=0`, cross-compiled for four platforms
from one Ubuntu runner, which is why `modernc.org/sqlite` is the driver and why
the Windows service is a single `.exe`. Turning cgo on in `lancastd` breaks all
of that at once, for a feature only picture libraries use — so the cgo lives in
one binary that needs it, and `lancastd` launches it, hands it paths, and reads
JSON back from its stdout.

`cmd/lancast-faces` already links an ONNX runtime, already loads models from a
directory the server manages, already has an install flow that names every
asset's size, licence and address before fetching it, and already writes
embeddings back as little-endian float32 blobs. Semantic search needs all four
of those and invents none of them.

What it needs that does not exist yet is **one more model, run over the whole
photograph rather than a detected face, plus the same model run over a line of
typed text.** That is the feature.

The binary is renamed in no way and the child-process contract does not change
shape. A worker that already answers "what faces are in these files" learns to
answer "what does this file look like" and "what does this sentence look like".

## No vector index, and the arithmetic says why

Immich needs VectorChord because Immich is built to scale past what one
household owns. LANcast has a measured ceiling — ADR 0057 benchmarks a
40,000-item library, "about twice the largest real library measured here
(18,777 items)" — and at that size the index is the thing you can skip.

A CLIP embedding is 512 float32s, 2KB. Forty thousand photographs is **82MB of
vectors**, and one search is 40,000 cosine similarities — about 20 million
multiply-adds, which is milliseconds of arithmetic.

That is the same shape `internal/store/face.go` already runs: `cosine()` over
embeddings pulled from SQLite, brute force, no index, because clustering
compares everything to everything anyway.

**The cost is the read, not the maths**, and that is the number to measure
before believing any of this — 82MB out of SQLite per query is not free, and the
answer is probably to hold the vectors in memory for the life of the process
the way nothing else here does. *Measure it against a real library before
choosing.* This project has a rule about that and a release that paid for
ignoring it.

### Measured, 2026-09-04: the read is the cost, and it is worse than this said

`BenchmarkSearchPhotos` in `internal/store/photoembedding_bench_test.go`, on a
Ryzen 9 3900X, one query returning the top 60:

| photographs | per search |
| --- | --- |
| 1,000 | 45 ms |
| 10,000 | 582 ms |
| 40,000 | **2.97 s** |

Linear, at about 74µs per photograph. "Milliseconds of arithmetic" was right
about the arithmetic and wrong about the total by three orders of magnitude:
a CPU profile puts 74% of the time inside SQLite's VDBE and essentially none in
`cosine`.

Two hypotheses were tested and both were wrong. The query plan is already the
good one — `SEARCH mi USING INDEX idx_item_library` then `SEARCH e USING
PRIMARY KEY` — so it is not a missing index. Replacing the join with a flat
scan of `photo_embedding` plus an in-memory set of eligible ids was **11%
faster**, which is noise against a factor of sixty: it is not the seeks either.

It is simply the throughput of pulling 20MB of blobs through the pure-Go
driver, about 37MB/s. Nothing about the query shape changes that, which means
**no amount of SQL is going to fix it** — the fix is to stop reading the
vectors per query, exactly as this ADR guessed.

What that costs is now a known number rather than a guess: 82MB of resident
memory for a 40,000-photograph library, held for the life of the process. That
is a real ask of a home server and it is **not** decided here. What is decided
is that the choice is now informed: at 2,601 photographs — the test library,
measured end to end — a search takes about 0.2s and needs nothing, and the
in-memory copy only starts earning its place somewhere above ten thousand.

Two things worth knowing before that decision is made. The ~2s the sidecar
spends starting a process and loading the text model is charged to **every**
search regardless, so caching the vectors takes a 40k search from 4.2s to about
1.3s rather than to nothing — the process, not the read, is then the ceiling.
And a cache has to be invalidated by the two things that change what a library
holds: an indexing pass, and a folder being marked sensitive. The second is the
one that would be forgotten, and forgetting it serves marked photographs from
memory after the rule has deleted them from the database.

### Measured, and taken: the process was the cost, not the read

The measurement above put the read at 2.97s for 40,000 photographs and left the
sidecar's own cost as a footnote. Measured properly, the footnote was the larger
number for every library anyone actually has:

| | per search |
| --- | --- |
| a process per query (as first built) | ~2.09 s |
| a warm process, marginal query | **~0.06 s** |

Startup is about 2.0s of loading the text model to do 60ms of arithmetic, and
it was being paid on every search at every library size. Consistent across runs
of 1, 20 and 100 queries.

Measured running as the service, a warm worker is **about 700MB resident** —
the model is 254MB on disk and the ONNX runtime is the rest. That is a good deal
more than the 250MB this was designed against, and it makes the idle exit the
load-bearing half of the design rather than a courtesy.

So the long-lived worker this ADR named as the fallback is now what runs, and
it is worth being clear that it was chosen over the in-memory cache on evidence
rather than taste. On the 3,015-photograph library this was built against, a
search was 2.09s of process plus 0.22s of reading: the worker removes 2.03s of
that and the cache would have removed 0.22s. It is also the option with no
correctness surface — a warm process returns the same vector a cold one does,
where a cache has to be invalidated by a pass, a marked folder, a missing file
and a model change, and the second of those is a privacy rule.

**This changes when the cache becomes worth it, and the honest reading is
"sooner".** With the process cost gone the read is now essentially the whole of
a search, so the 2.97s at 40,000 photographs is no longer hidden behind
anything. The order of preference in the previous section still holds — halve
the read with float16 before eliminating it with a cache — but the trigger is
now library size alone, and nothing here has measured float16.

Two properties of the warm worker are load-bearing rather than incidental. It
**exits after five minutes of quiet**, because a media server holding 700MB
permanently for a feature used twice a month has taken something that is not
its to take; a burst of searching pays the load once at the start of it. And
the protocol is **order, not request ids** — one line in, one line out — so
every exchange holds a lock through reading its own answer. A desynchronised
pipe would not crash: it would answer each search with the previous search's
vector, and every result would be a confident, correctly-ranked list of
photographs for something nobody asked, reported by nothing.

The consequence worth stating plainly: **adding a vector extension would mean
giving up the pure-Go driver**, which is the same trade ADR 0052 refused for
cgo. A feature that costs the release matrix is a feature that costs every user
for the benefit of some.

## Which model is decided by its licence, not its accuracy

ADR 0052's first draft named SCRFD and ArcFace — "what everybody reaches for and
is the wrong answer here" — because InsightFace's *code* is MIT while its
*pretrained weights* are non-commercial research only. LANcast ships under
AGPL-3.0 with a commercial licence available (ADR 0053), so weights that forbid
commercial use are weights that cannot ship.

The same table, built for **weights** rather than repositories:

| weights | licence | usable |
|---|---|---|
| [`openai/clip-vit-base-patch32`](https://huggingface.co/openai/clip-vit-base-patch32) | **none declared** | **no** |
| [`laion/CLIP-ViT-B-32-laion2B-s34B-b79K`](https://huggingface.co/laion/CLIP-ViT-B-32-laion2B-s34B-b79K) | **MIT**, with a caveat — see below | yes |
| [`google/siglip-base-patch16-224`](https://huggingface.co/google/siglip-base-patch16-224) | **Apache-2.0** | yes |

**OpenAI's own weights declare no licence at all**, which is the worst of the
three and the opposite of what a reader assumes from a famous open model: no
declared licence is no grant. They are out.

**The LAION caveat is a caveat, not a licence term, and the distinction is the
whole reason this section exists.** The card's licence field says MIT; its
*Out-of-Scope Use* section says "Any deployed use case of the model — whether
commercial or not — is currently out of scope". That sentence is inherited from
OpenAI's original card and is about untested deployment domains rather than
permission, and [open_clip#503](https://github.com/mlfoundations/open_clip/issues/503)
is somebody asking exactly this and being pointed at a prior thread concluding
commercial use is fine.

That is a reading, and it is recorded as one. **If it is read as binding
instead, SigLIP is the answer and nothing else in this ADR changes** — the
shape, the storage and the exclusions are all model-independent, which is most
of why ONNX was chosen in the first place.

### The tokenizer is the other half of the choice, and it points the other way

Nothing here needs a text encoder until a person types a sentence, and then it
needs the *same* model's tokenizer or the two vectors are not comparable.

- CLIP and OpenCLIP use **byte-pair encoding** with a fixed 49,152-token vocab.
  Well-specified, deterministic, a few hundred lines and a vocabulary file.
- SigLIP uses **SentencePiece**, a protobuf-described unigram model. Correct
  implementations in Go are a dependency rather than an afternoon.

So the cleaner licence has the harder tokenizer and vice versa.

**Chosen: OpenCLIP ViT-B/32, MIT.** 512 dimensions, which is the number the
arithmetic above is built on, and a tokenizer this project can write and test
the way it wrote its own EXIF reader rather than take a dependency for. SigLIP
stays the documented fallback, and swapping is a file and a tokenizer — not a
rebuild — which is exactly the property ADR 0052 bought with ONNX.

The model directory, the install flow and the "a download somebody cannot
identify is not consent" rule already exist and are reused unchanged.

## What is stored, and where

An embedding per photograph, in the shape faces already uses — a BLOB of
little-endian float32s. `face.go` gives the reason for that encoding and it
carries over unchanged: explicit rather than gob or JSON, because the blob is
written once and read on every comparison, and "a self-describing format would
cost size and speed for a shape that is fixed by the model."

It hangs off `media_item` with `ON DELETE CASCADE`, for the reason the face
table gives: an embedding is derived from a photograph and is not less private
than it, so it goes when the photograph goes.

Schema revision and migration required, which is why this is an ADR.

## Marked folders are never embedded

Not merely never returned — never computed, and deleted if a folder is marked
afterwards. This is `DeleteFacesUnderSensitive` applied to a second kind of
vector, and the argument is ADR 0051's own, already made twice: an embedding is
derived from the photograph and is not less private than it, so a stored one
would sit in the database and in every backup taken afterwards.

There is a sharper version of it here. A face embedding says *who*; a CLIP
embedding says *what the picture is of*. A marked folder that could be reached
by searching for what is in it would be a cover that lifts for anyone who
guesses the contents.

## What this does not decide

**Search across libraries, or over anything but photographs.** Films and tracks
have titles somebody meant; the gap is specific to pictures.

**Ranking against the existing `?q=`.** Whether semantic results merge with
title matches or sit beside them is a browse-experience decision and should be
made where that lives, not here.

**Whether the index is built eagerly or on demand.** The face pass is started by
an administrator per library and reports through `/api/activity`; this can
follow it, and should, unless measuring says otherwise.

## Consequences

The good: the one unsearchable library becomes searchable, entirely on the box,
with no account and nothing leaving the machine — which is the same sentence
face grouping earned and the sharpest thing this product can say.

The cost: a second model to download (~100–300MB depending on which), a second
pass over every photograph, and a permanent 2KB per photograph in the database
and therefore in every backup. On the 18,777-item library measured in ADR 0057
that is under 40MB, against a database already at 103MB.

The risk: **the model is the feature.** ADR 0052 chose ONNX so quality is a file
swap rather than a rebuild, and the same applies here — but a search that
returns the wrong pictures is worse than no search, because it teaches people
the feature does not work and they stop trying. It wants measuring against a
real library and a real question before it ships, not a benchmark.
