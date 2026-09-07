# ADR 0062 — A tag is a note to yourself

Date: 2026-09-07 · Status: **proposed**

Extends ADR 0002 (one wide `media_item`), ADR 0006 (per-user state), ADR 0008
(field-level locking) and the design system's gold rule. Supersedes nothing.

## Context

LANcast has genres and it has ratings. It has no way for a person to say
anything about an item in their own words, and no way to mark one as a
favourite — the only "favourite" in the codebase is per-device channel pinning
from ADR 0039.

Genres are not a substitute. They arrive from a provider, a refresh overwrites
them, and the vocabulary is somebody else's: there is no TMDB genre for *needs a
better copy*, *christmas*, or *watch before the sequel*.

The Immich study put this fifth. Two of its claims do not survive contact, and
both corrections are the useful part of this decision.

### The locking argument does not apply

The study said tags are "a locked-field problem, and ours is already solved" —
ADR 0008's field locking being the mechanism a tag system needs.

That holds only while a tag can arrive from something other than a person.
Locking exists because a provider refresh and a human edit compete for one
field and the human must win. **Nothing competes for a tag.** No provider emits
one, no scan reads one, and with sidecars off the table (below) the only writer
is somebody typing.

So no lock is installed, and that is not an omission. A lock guarding a writer
that does not exist is a mechanism nobody can test and nobody maintains — and on
the day something real does start writing tags, a lock put in years earlier "for
safety" is exactly the sort of thing that turns out to have been guarding the
wrong column.

### Sharing was the wrong instinct, and it was mine

The first draft made tags shared across the household, reasoning from the shape
of the model: genres, playlists and collections are shared, so organisation is
shared, and a tag nobody else can see fails at helping somebody find the thing.

That reasoning is about a library. A tag is not about a library — it is about the
person writing it. *Needs a better copy* is a judgement. *Watch before the
sequel* is a plan. *Do not delete* is an argument somebody expects to have.
Together they are a fairly complete picture of what somebody is doing and
thinking, and the whole of it would have been visible to everybody in the house
because the data model happened to have a table shaped that way.

Corrected by the person who lives with it, before any rows existed. Which is
what the "what would make this wrong" section was for.

## Decision

### Tags belong to one account and are visible to nobody else

Each account has its own tags. You see yours; you never see anybody else's, and
nobody can alter or remove yours.

This joins favourites, watch history, progress and `user_rating` on the personal
side of a line the model already draws. The two features in this ADR are now the
same shape, which is a simplification rather than a coincidence: both record
what one person thinks about something, and neither describes the item.

**The cost is accepted rather than mitigated.** Tag something *christmas* and
nobody else in the house benefits; if two people want the same shelf they build
it twice. That is a real loss and it is smaller than the alternative, which is a
feature people use carefully because they know it is read.

### The vocabulary is per-account too, not a shared table with per-account rows

The tidier schema is one `tag` table of names with a per-user join, the way
`genre` and `item_genre` work. It is refused, and the reason is the whole point
of the decision.

A shared vocabulary **leaks the vocabulary**. Anything that lists tag names — a
filter row, a facet response, an autocomplete — either returns everybody's names
or has to remember to filter by owner, at every call site, for ever. The first
one that forgets shows Georgia the existence of a tag called *sell these* even
though she can reach none of the items under it. The existence *is* the private
part.

So ownership is on the tag itself: `tag(id, user_id, name)`, unique per account.
Two people using the word *christmas* have two rows, which is duplication a
normaliser would object to and a privacy boundary would not. **Privacy that
depends on every query remembering to filter is not a boundary, it is a habit.**

### Tags live in the database and nowhere else

No NFO, no XMP, no sidecar.

They then cannot be disturbed by a rescan **by construction rather than by a
guard**, which is stronger than locking because there is no writer to lose a race
with. A backup already holds them (ADR 0058) — they are precisely what that ADR
described, a correction no rescan can rebuild.

Against that: not portable to another tool, and a database loss without a backup
loses them. The same bargain watch history, ratings and playlist edits already
accept. And it matters more now that tags are private: `write_nfo` puts files
next to media, where anybody with the drive can read them, which would make a
private note the least private thing in the system.

### A favourite is a shape, never a colour

`docs/design.md` names "favorite" explicitly as a thing gold must never mean, and
gold is the system's only accent. Introducing a second accent to carry
favourites would answer the letter of that rule and break its purpose: gold means
one thing precisely because nothing else competes for the eye.

So the affordance carries no hue at all — **an outline heart at rest, filled when
set**, both in the existing text ramp. Shape does the work once colour is spent.

A star is refused: the detail page already shows a star rating, and one glyph
meaning both "I rated this four" and "I like this" is worse than no affordance.

### A tag's identity is its name, folded for comparison and kept for display

`Christmas`, `christmas` and `Christmas ` are one tag, within one account.
Stored with the first spelling that account used, matched case-insensitively
after trimming and collapsing internal whitespace.

Not routed through `internal/media`'s `clean`/`SortTitle`. That is the *title*
normalizer, and CLAUDE.md's one-normalizer rule is about not having two
disagreeing answers to the same question — this is a different question, and
forcing a title normalizer onto it would give tags opinions about articles and
sort order.

### A tag with nothing tagged stops existing

Removing the last item from one of your tags removes that tag. Otherwise the
filter list accumulates every typo anybody ever made, and a feature meant to make
things findable becomes a thing to scroll past.

### Any account may tag, including through an API key

Tagging is not administration — it changes what is written about an item, not
what the server can reach — so it is not behind `adminOnly`, and an API key may
do it (ADR 0061). A key acts as its owner, so it writes that owner's tags and
sees no others; a script that tags what it imports is a good reason for keys to
exist.

## Consequences

**Schema revision 43**, additive: `tag(id, user_id, name)`, an `item_tag` join,
and `user_favourite` keyed by account and item. A downgrade survives it.

**Every tag read is scoped by the caller's account**, including the facet and
filter paths. That is a property worth a test rather than a convention, because
it is the kind that stays correct until somebody adds the second call site.

**Additive API** (ADR 0018), both halves of the contract in the same commit.

**What would make this wrong.** If the house ends up wanting a shared shelf —
one list of *christmas* that everybody maintains — this cannot become that by
loosening a flag, because the rows are owned. It would need a second concept
beside tags, which is the right shape for it anyway: a shared thing people
curate together is a collection, and LANcast already has those.
