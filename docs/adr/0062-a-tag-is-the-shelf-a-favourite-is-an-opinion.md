# ADR 0062 — A tag is the shelf; a favourite is an opinion

Date: 2026-09-07 · Status: **proposed**

Extends ADR 0002 (one wide `media_item`), ADR 0006 (per-user state), ADR 0008
(field-level locking) and the design system's gold rule. Supersedes nothing.

## Context

LANcast has genres and it has ratings. It has no way for a person to say
anything about an item in their own words, and no way to mark one as a
favourite — the only "favourite" in the codebase is per-device channel pinning
from ADR 0039.

Genres are not a substitute. They arrive from a provider, they are overwritten
by a refresh, and their vocabulary is somebody else's: there is no TMDB genre
for *needs a better copy*, *christmas*, or *the ones Georgia likes*.

The Immich study put this fifth and made two observations. One of them turns out
to be wrong here, and it is worth saying why before building on it.

### The locking argument does not apply, once sidecars are off the table

The study said tags are "a locked-field problem, and ours is already solved" —
ADR 0008's field locking being the mechanism a tag system needs.

That is true only if a tag can arrive from somewhere other than a person. Locking
exists because a provider refresh and a human edit compete for one field, and the
human must win. **Nothing competes for a tag.** No provider emits one, no scan
reads one, and the only writer is somebody typing.

So field locking is not wired in, and that is not an omission. A lock that
protects a field from a writer that does not exist is a mechanism nobody can
test and nobody will maintain — and the day something *does* start writing tags,
a lock installed years earlier "for safety" is precisely the sort of thing that
turns out to have been guarding the wrong column.

## Decision

### Tags are shared; favourites are per-account

The model already draws this line and it is worth following rather than
reinventing:

| shared | per-account |
| --- | --- |
| `genre`, `item_rating`, playlists, collections | `user_rating`, watch history, progress, sharing |

A tag is **organisation** — a property of the shelf, like a collection. Two
people tagging the same film separately is the same work done twice, and a tag
one person cannot see is a tag that failed at the only thing it was for: helping
somebody else in the house find the thing.

A favourite is an **opinion**, and this project already keys opinions by account.
"Our favourites" is a strange object for two people to own jointly, and the row
that stores it should look like `user_rating`, because it is one.

The cost is real and stated: anybody signed in can remove a tag somebody else
added. That is already true of playlists and collections, the audit log records
who did it, and the alternative — a private namespace per account — is a library
organised twice by people who cannot see each other's work.

### Tags live in the database and nowhere else

No NFO, no XMP, no sidecar. Two consequences, one good and one to be honest
about.

They cannot be disturbed by a rescan, **by construction rather than by a
guard**, which is a stronger guarantee than locking: there is no writer to lose
a race with. And a backup already holds them (ADR 0058) — they are exactly the
category that ADR called out, a correction a person made that no rescan can
rebuild.

Against that: they are not portable to another tool, and losing the database
without a backup loses them. That is the same bargain watch history, ratings and
playlist edits already accept, and writing to a person's media folders to avoid
it is a larger thing to do than the portability is worth. A scan with
`write_nfo` on already writes sidecars next to real media; adding user-authored
tags to that stream widens a blast radius the project deliberately keeps narrow.

### A favourite is a shape, never a colour

`docs/design.md` names "favorite" explicitly as a thing gold must never mean,
and gold is the system's only accent. Introducing a second accent to carry
favourites would answer the letter of that rule and break its purpose — the
reason gold means one thing is that nothing else competes for the eye.

So the affordance carries no hue at all: **an outline heart at rest, a filled
heart when set**, both drawn in the existing text ramp. Shape does the work,
which is what is left once colour is spent.

A star is refused: the detail page already shows a star rating, and one glyph
meaning both "I rated this four" and "I like this" is a worse problem than no
affordance.

### A tag's identity is its name, folded for comparison and kept for display

`Christmas`, `christmas` and `Christmas ` are one tag. Stored with the first
spelling somebody used and matched case-insensitively after trimming and
collapsing internal whitespace — the same shape `genre.name UNIQUE` has, with
the folding that genres do not need because a provider is consistent and a
person is not.

Not routed through `internal/media`'s `clean`/`SortTitle`. That is the *title*
normalizer, and CLAUDE.md's one-normalizer rule is about not having two
disagreeing implementations of the same question — this is a different question,
and forcing a title normalizer onto it would mean tags acquiring opinions about
articles and sort order.

### A tag with nothing tagged stops existing

Removing the last item from a tag removes the tag. The alternative is a filter
list that accumulates every typo anybody ever made, which is how a feature meant
to make things findable becomes a thing to scroll past.

### Any account may tag, including through an API key

Tagging is not administration — it changes what is written about an item, not
what the server can reach — so it is not behind `adminOnly`, and an API key may
do it (ADR 0061). A script that tags everything it imports is a good reason for
keys to exist.

## Consequences

**Schema revision 43**, additive: a `tag` table shaped like `genre`, an
`item_tag` join, and a `user_favourite` table keyed by account and item. A
downgrade survives it.

**Additive API** (ADR 0018), both halves of the contract in the same commit.
Tags join the existing filter and facet machinery, so they are reachable the way
genres already are rather than through a parallel surface.

**What would make this wrong.** If it turns out people want tags to be private
after all — if the first real use is somebody labelling things they would rather
not explain — then the sharing decision is the mistake, and moving it later means
migrating rows that already exist under one meaning into another. Worth watching
for, and cheaper to hear about early than to guess about now.
