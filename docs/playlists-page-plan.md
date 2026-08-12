# A playlists page — plan

Status: proposed · 2026-08-12 · follows [ADR 0030](adr/0030-playlists-and-m3u.md)

## The problem this fixes

Playlists exist, can be edited, and are **almost impossible to find**.

A playlist is an ordinary `media_item` with `kind = 'playlist'`, filed in the
library its `.m3u` was found in. A music library's top level is *artists*
([ADR 0024](adr/0024-music-libraries.md)) — so a playlist is not on it. The only
ways to reach one today are a search that happens to match its name, or a
recently-added shelf that happens to still list it. Both are accidents.

That is the same failure the "Add to playlist" button had in v0.6.10, one level
up: a capability that cannot be reached is not a capability. Editing was the
half we could build without deciding anything new; being able to *find the
thing you edited* is the half that needs a screen.

## Entry point

**A "Playlists" control at the top of a music library**, beside Play all and
Shuffle, going to `/library/{id}/playlists`.

Not a top-level nav item, and not a tab strip:

- **Not global.** A playlist belongs to a library — that is where its rows and
  its `.m3u` live — and a global page would have to invent a grouping for
  playlists from three libraries, or silently mix them. It can become global
  later if per-library turns out to be the wrong grain; the reverse is harder.
- **Not a tab strip on the library header.** The header already carries search,
  a sort, a facet row, and two play controls. A tab strip would compete with the
  facet row for the same line and the same gesture, and facets are what people
  are actually reaching for on a library screen.
- **Only where playlists make sense.** A `LibraryKindConfig` flag (`playlists:
  true`), on for music, off for films and pictures until someone asks. A film
  library *can* hold a playlist and nothing stops it; it is simply not what the
  control is for, and a dead button in every library is spent space.

## What the page shows

A grid of playlist tiles, reusing `PosterTile` and the browse shell — the same
argument that made playlists browsable on day one.

Each tile needs the thing the grid cannot currently show: **how long the list
is**. `child_count` is 0 for a playlist by design (it counts `parent_id`
children, which a playlist has none of), so the count has to come from the
entries. Two options, and the first is preferred:

1. **Extend the item listing to fill `child_count` for playlists from
   `playlist_entry`.** One query in `AttachChildCounts`, keyed on the same
   listing that already runs. The field then means "how many things are in this"
   for every kind, which is what a client already assumes it means. This
   contradicts a sentence in `docs/api.md` that says a playlist's `child_count`
   is 0 — that sentence would be corrected, and it is a *widening*, not a break:
   nothing can be relying on a constant zero.
2. A `?count=true` parameter on the playlist listing. Cheaper to reason about,
   worse to use, and it invents a second way to ask a question the API already
   answers.

**Artwork** is the second gap. A playlist has none of its own and nothing will
provide one. Compose a tile from the first up-to-four entries' covers — the
familiar quilt — computed client-side from the entries already fetched, with a
plain lettered tile as the fallback for an empty list. No new server surface, no
new cache entries, nothing to invalidate.

## What the page does

Everything the playlist detail page already does, plus the two things that have
nowhere to live today:

- **New playlist** — a name field, the same one the Add-to-playlist dialog uses,
  creating in this library. Today a playlist can only be born as a side effect
  of adding a track to it, which is backwards for "I want a list for tonight".
- **Rename** — `PATCH /api/items/{id}` with a title. The endpoint has existed
  since M2; no client has ever called it for a playlist.
- Open, play, delete — already built, already routed.

Deliberately **not** on this page: reordering. That belongs on the playlist
itself, where the sequence is visible; a grid of tiles is the wrong surface for
an ordered list of tracks.

## Server work

Small, and most of it is already there.

- `AttachChildCounts` learns `playlist_entry` (option 1 above), and `docs/api.md`
  loses the sentence saying a playlist counts zero.
- Nothing else. Create, append, replace, remove-entry and delete all shipped in
  v0.6.10, and the page is a client assembled from them.

## Open questions, and why they are not blocking

- **Per-user playlists.** ADR 0030 left this undecided and the page does not
  force it: a server-wide list renders identically to "my lists" when there is
  one account, and the page gains a filter rather than a redesign if ownership
  arrives.
- **Smart playlists** (rules rather than members) want their own ADR. A
  rule-backed playlist is not an ordered list of ids and does not fit
  `playlist_entry`; the page should not grow a "new smart playlist" button until
  that decision exists.
- **Writing `.m3u` back to disk.** Out of scope, and ADR 0030's round-trip
  argument is the thing to re-read first if it is ever asked for.

## Estimate

One session. The page is a browse shell with a different query, two dialogs
already written, and one store method extended. The design risk is the tile
artwork, which is the only thing here that has never been drawn.
