# ADR 0093 — A collection knows its missing films

Date: 2026-10-25 · Status: accepted

## Context

A collection ([ADR 0017](0017-collections-and-multi-part-works.md)) is a
membership table: `item_collection` links films that are in the library to the
franchise a provider says they belong to. It can answer "what here belongs
together" and nothing else. Asked for a better collection page, the first thing
missing was the question every box set raises: *which ones have I not got?*

The provider already answers it. TMDB's `/collection/{id}` lists every film in
the franchise, and LANcast was already asking that endpoint for every
collection — first for its name in the right language, then for its
description — and throwing the list away.

## Decision

**Store the provider's film list beside the membership table, not in it.**

```sql
CREATE TABLE collection_film (
    collection_id INTEGER NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    provider      TEXT    NOT NULL,
    external_id   TEXT    NOT NULL,
    ord           INTEGER NOT NULL,   -- release order
    title         TEXT    NOT NULL,
    year          INTEGER,
    released_at   INTEGER,
    overview      TEXT,
    poster_hash   TEXT,               -- only for films not in the library
    PRIMARY KEY (collection_id, provider, external_id)
);
```

Four rules come with it.

1. **Owned is decided when it is read, never stored.** A listed film is owned
   when a present `movie` in the collection's library carries the same
   provider and external id. Storing it would be a second copy of a fact the
   library already holds, and it would go stale the moment a film was added:
   a sequel scanned tonight is owned tonight, with no list refresh.
2. **The list is the provider's, replaced whole.** A refresh deletes and
   rewrites a collection's rows, so a newly announced sequel arrives and a film
   the provider dropped leaves. A failed lookup is *not* an empty list and
   writes nothing: "the provider did not say" never overwrites what it said
   yesterday.
3. **Posters only for what is missing, once.** An owned film has its own
   poster. A missing one gets one image fetch, into the same content-addressed
   cache, kept across refreshes. The artwork prune counts `poster_hash` as
   referenced, or it would delete every one of them.
4. **A rating ceiling hides missing films.** A film that is not here has no
   certificate to test against the ceiling, and the restricted sequel is
   exactly the film most likely to be missing from a child's view. So an
   account with a ceiling sees the owned films it may open and no missing ones.
   An owned film it may not open is left out, as every listing leaves it out.

Collections created before this get a list without anybody pressing Refresh: at
the end of an enrichment pass (which the server starts at boot), every provider
collection with no list is looked up by its own id, through an optional
`meta.CollectionProvider` capability — the same pattern as trailers. That costs
one request per collection, once, rather than re-enriching every member film.

Keyword groupings (the MCU, the MonsterVerse) have no list and never will: a
keyword is a tag, not a TMDB collection. Their page simply shows no missing
films.

## Consequences

**Good.** The page can say "3 of 5" and show the two that are not here, for no
provider requests beyond the ones already being made.

**Good.** Because ownership is computed, nothing has to keep it consistent. The
table holds only what the provider said.

**Cost.** One more table and one more reader of the artwork cache's liveness,
which is the dangerous direction: a liveness query that forgot this table
would delete pictures. It has a test.

**Cost.** Ownership matches on provider id. A film in the library matched to a
different provider, or unmatched, reads as missing even though the file is on
disk. That is honest about what LANcast knows, and fixing the match fixes it.

## Revisit if

A second provider supplies collection lists that disagree with TMDB's. The
primary key already carries `provider`, so two lists can coexist; deciding
which one the page shows is the open question.
