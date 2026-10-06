# ADR 0075 — A duplicate is the same bytes

Date: 2026-10-04 · Status: **accepted**

## Context

"Photos need more than a grid" listed duplicate detection as unbuilt. Before
designing it, every photo in a real picture library — 3,079 of them, 5.7 GB —
was hashed read-only with the server's own decoder, two ways: a SHA-256 of the
file, and a perceptual difference hash (dHash) of the oriented image.

**Exact copies: 38 groups, 44 extra files.** And most of them were not
mistakes. They were the same photo filed in two albums — `Animals/` and
`Me & Us/`, `Denver Trips/` and `Nature/` — which in a picture library, where
the folder *is* the album (ADR 0028), is somebody's filing. A small number were
genuine accidents: `IMG_…(1).jpg` beside `IMG_….jpg` in one folder.

**Near copies could not be found reliably.** Seventeen pairs across the dHash
distance range were judged by eye:

| pairs judged by eye | 64-bit distance | 256-bit distance |
|---|---|---|
| the same picture, re-saved or resized | 0, 0, 2, 3, 6 | 4, 28, 19, 22, 28 |
| different screenshots of the same screen | **2**, 4, 4, 5, 7, 8 | **9**, 52, 43, 41, 41, **22** |
| bursts a second apart | 5, 6, 8 | 41, 24, 53 |
| different objects altogether | 7, 10, 10 | 62, 80, 77 |

The ranges overlap at both resolutions. Two different screenshots of a dark
interface sat closer than a real resized copy did, so no threshold separates
"the same photo" from "a similar photo" on this library.

## Decision

**1. A duplicate is two photos with the same SHA-256.** Nothing else. It is
never wrong, and it is what the measurement can stand behind.

**2. The digest lives in its own table, `photo_hash(item_id, sha256)`**,
written by the photo worker from the read it already makes for the thumbnail,
so it costs a hash rather than a second pass over the library. Its own table for
the reason markers have one: it is derived, recomputable, and goes with the row
(`ON DELETE CASCADE`). Revision 58 sends every photo back through the worker
once so the existing ones get a digest.

**3. A changed file is read again.** The scanner upserts a file only when its
size or mtime moved, and a digest is a fact about bytes, so the upsert drops it
and re-queues the photo. This also fixes an older gap: an edited photo's
thumbnail had never been regenerated.

**4. Every copy is reported with its album, and nothing picks which to keep.**
`GET /api/libraries/{id}/duplicates` returns groups, largest first, each copy
with the gallery it sits in (`null` at the library root). The page says, before
anything is removed, what removing a copy does: *"This removes the photo from
"Animals". A copy stays in "Me & Us"."* — or, for two copies in one folder,
*"Another copy stays in "Resin Art"."*

**5. Removal is the ordinary removal.** The same dialog and the same
`DELETE /api/items/{id}` as everywhere else: admin only, `ignore` keeps the file,
`delete` is refused when media deletion is off. The endpoint grants nothing; it
lists what anyone who can browse the library can already see.

**6. Marked photos take no part** (ADR 0051), as they take no part in the
timeline. Listing a marked copy beside an unmarked twin would show the picture
the mark exists to obscure, and with the marked copy gone its twin has no
partner.

## Consequences

The duplicates list and the timeline are refreshed when anything is removed. The
timeline was not before — a removed photo stayed counted until something else
refreshed it.

A library still being processed reports only what the worker has read, and the
page says so rather than claiming there are no duplicates.

## Alternatives rejected

**A perceptual hash with a threshold.** Measured above: no threshold works on
this library, and a duplicates page that offers to remove a different photo is
worse than one that misses a resized copy.

**Picking the copy to keep** (largest, oldest, the one in the "best" folder).
Most groups are deliberate filing, so there is no copy to prefer — only an album
to leave.

## Not decided here

**Near copies** — the same picture resized or re-saved. The strongest signal on
the box is the image embedding the faces worker already computes for semantic
search. Whether its similarity separates a resized copy from a similar
screenshot is a measurement to make before anything is built on it. **Bursts** —
choosing the best of several shots a second apart — are a different feature
again.

## Amendment — 2026-10-06: near copies, measured

The question left open above, measured on the same picture library (now 3,073
photos, every one with a search embedding). It is read-only: a copy of the
database, and the cached upright display copies.

**Method.** Every pair of photos was scored on four things:
- cosine similarity of their semantic-search image embeddings;
- the 64-bit dHash distance of their display copies;
- whether they are in the same folder;
- the gap between their capture times.

There were 2,319 pairs at cosine 0.85 or above. The 43 exact copies on the
Duplicates page all scored 1.0 with a dHash of 0, which calibrates the
instrument. 106 further pairs were then judged by eye:
- every pair at 0.97 or above;
- a sample at 0.95–0.97;
- every pair the final rule selects;
- every pair below 0.93 that passes its other filters.

**What sits close in embedding space, apart from copies.**
- **Bursts** are the bulk of it. They are shots taken a second or more apart,
  and many are indistinguishable at thumbnail size. One family-room burst alone
  produced nine pairs at 0.97. The embedding cannot separate a burst from a
  re-save. The **capture time** can: a burst has a gap of 1 second or more, and
  a re-save of the same file has the same timestamp or none.
- **Crops** of the same picture (a square profile crop of a portrait) score
  0.95–0.99 with dHash 14–28, because the framing changes the hash.
- **Variants**: a wallpaper in two colourways, or different screenshots of the
  same application. Screenshots are the hard case ADR 0075 already named. Two
  different screenshots of one dark interface scored 0.9885 with dHash 6.
- **Edits** sit below 0.93. These are the same photo with names painted on it,
  or with a colour filter.

**The rule that holds.** A pair is a near copy when:
- embedding cosine ≥ 0.93;
- dHash distance ≤ 8;
- the capture gap is zero or unknown;
- they are not both screen-sized. Screenshots are left out entirely.

On this library that is **32 pairs in 29 groups: 60 photos, 31 extra copies**.
Judged by eye, 30 are plainly the same picture resized or re-saved, for example
3648 px beside 720 px, or 1000 px beside 319 px. One is uncertain: a group
photo at the same size, either a re-save or a burst with no EXIF. **None is a
different picture.** 24 of the 32 pairs are in the same folder, which makes
them accidental saves rather than filing.

What it does not find:
- crops, edits and bursts, by design;
- one logo saved at two sizes, whose capture times are file dates a day apart.

**What this changes.** Near copies are now measurable, and decision 1 still
stands: a *duplicate* is the same bytes. If a near-copy section is built, it
should keep these properties:
- **separate from exact duplicates**, named as "probably the same picture";
- **never acted on automatically**, with both shown at their real sizes and
  the larger marked as the one to keep;
- **built on stored signals.** The dHash would be written by the photo worker
  beside the SHA-256, from the decode it already makes. The embedding exists
  only once search has indexed the library, so the section says it is waiting
  rather than showing an empty page.

Bursts, which means choosing the best of a run of shots, remain the separate
feature named above. On this library the capture-time gap measured here is
the boundary that would define one.

*Built 2026-10-06.* `GET /api/libraries/{id}/near-copies` applies exactly the
rule above. On the Duplicates page it is a third section, "Probably the same
picture", after the exact groups:
- each version shows its size, and the largest is labelled (not in gold);
- removal goes through the ordinary dialog, with a sentence naming the size
  that stays.

The dHash is written by the photo worker into `photo_hash.dhash` (revision 60),
from the upright fitted picture it already makes for the grid. Revision 60
sends every photo through the worker once to fill it in.

Run against a migrated copy of the measured library, the shipped code returned
the measurement's answer exactly: 29 groups, 60 photos, 31 extra copies.
