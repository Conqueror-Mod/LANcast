# ADR 0123 — A library carries its own options

Date: 2026-11-08 · Status: **accepted** 2026-11-08 · Related: [ADR 0101](0101-a-comic-is-read-out-of-its-archive.md), [ADR 0120](0120-a-setting-belongs-to-what-it-describes.md), [ADR 0122](0122-metadata-may-be-in-another-language.md), [docs/settings-3-plan.md](../settings-3-plan.md)

## Context

ADR 0120 says a preference about media belongs to the media, not to whoever
is reading or watching it. Two settings are about one library rather than the
whole server:

- **Reading direction.** Manga reads right to left. That is true of the book,
  not of the reader, and a server with an American comics library and a manga
  library beside it needs both directions at once. The reader only knew left
  to right (settings-3-plan step 4). Settings 3.0 decision 4 asked for it per
  library, with a per-series override, because a comics library is often
  mostly one kind with a few of the other.
- **Metadata language.** ADR 0122 made it a server setting and left per
  library to this step. An anime library fetched in Japanese beside films
  fetched in English is the case it is for.

Neither had anywhere to live: the `library` row holds a name, a kind, and
the folders.

## Decision

**Two nullable columns on `library` (migration 95):**

- `metadata_language` — a tag from `meta.Languages`. **Null follows the
  server's** `metadata_language`, so changing the server's setting still moves
  every library nobody set. Only film and show libraries take one; TMDB is the
  only provider whose answers depend on the language.
- `reading_direction` — `ltr` or `rtl`. **Null is left to right.** Only
  comics libraries take one.

Setting either on a library of another kind is a 400 rather than stored and
ignored: a value that does nothing is one somebody sets believing it worked.

**A series override in its own table, `comic_series_direction`**, keyed by
library and series name. A comic's series is already its folder's name
(`media_item.series`, ADR 0101), so the key is the one the library already
uses. A table rather than a column on each comic, because the override is
about the series: a new issue dropped into the folder must read the same way
without anybody setting it again. Removing the override deletes the row.

**The server resolves the direction; the reader is told it.**
`GET /api/items/{id}/pages` answers with `direction` — the series override,
else the library's, else `ltr` — and where it came from. A client does not
work out precedence for itself, so two clients cannot disagree about one
book.

**Right to left turns the whole reader round.** The next page is to the left:
the left arrow, a tap on the left, a swipe to the right, and the left-hand bar
button all go forward. In a spread the earlier page is on the right. Space and
Page Down still mean forward, because they have no side.

**The metadata language reaches the provider through the request context.**
The enrichment worker resolves an item's language from its library (falling
back to the server's) and places it on the context; the TMDB client reads it
before the client's own default. Every path that fetches for one item —
enrichment, Fix match, the by-id refreshes, trailers — goes through it, and
the provider interface is unchanged, so no other provider learns about
languages it does not have. The cache key already includes the language
(ADR 0122), so two libraries in two languages never share an answer.

**Changing a library's language offers the refresh ADR 0122 built**, for that
one library, and never starts it.

Only admins set either. A library's options are a fact about the library,
like its name, and every household member reads the same books the same way.

## Consequences

- Schema 95 is one-way, as every migration is: an older build refuses the
  database. The columns are nullable and default to the behaviour before this
  change, so nothing reads differently after the upgrade.
- A series' override outlives the series' comics. Renaming the folder starts
  a new series, which reads the library's way until it is set again. That is
  the same rule ADR 0101 uses for the series itself.
- Further per-library options (exclude patterns, detection on or off) can
  follow the same shape: a nullable column, null meaning "as the server says".
