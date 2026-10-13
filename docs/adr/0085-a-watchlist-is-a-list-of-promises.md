# ADR 0085 — A watchlist is a list of promises

Date: 2026-10-13 · Status: **accepted** 2026-10-13 · beside
[ADR 0062](0062-a-tag-is-a-note-to-yourself.md)

The plan is [settings-plan.md](../settings-plan.md), step 6: "a per-account
list of things to watch later, with a Home shelf". Chris decided two things on
2026-10-13:
- **A finished title leaves the list by itself.**
- **"Add to Watchlist" is a button on the title page as well as in the menus.**

## Context

Favourites (ADR 0062) are the nearest thing LANcast has: a per-account table,
`user_favourite`, with a heart on each item and a filter in browse. A
watchlist is the same shape with a different meaning. A favourite says "I love
this", and is kept for good. A watchlist entry says "I mean to watch this", and
is done once it is watched. Reusing favourites, or making the watchlist a tag,
would merge two things a person means differently, so it is its own table.

## Decision

1. **Its own table, private like tags.** `user_watchlist(user_id, item_id,
   added_at)`, keyed on both, cascading from `media_item`. Every query takes
   the account, so no caller can read somebody else's list. There is no
   foreign key to `user`, for the reason `session` and `passkey` have none:
   `DeleteUser` removes the rows in its own transaction.
2. **Films and shows only.** A season or an episode is part of a show you
   mean to watch, and a collection is a list in its own right. Anything else
   is refused with a 400, not stored.
3. **Finishing removes it** (Chris's decision). `SaveProgress` already sees
   the moment an item becomes watched, because it logs the viewing then. In
   the same transaction:
   - a film leaves the list;
   - a show leaves when that account has no unwatched episode left.

   Marking something watched by hand goes through the same write, so it does
   the same. Un-watching does not put anything back: a list that refilled
   itself would not be a list a person keeps.
4. **Newest first.** The list and the shelf are ordered by when each title was
   added. A to-do list is read from the top, and the top should be the thing
   just decided on.
5. **Account limits apply** (ADR 0084). The list goes through
   `PermittedItems`, so a title from a library hidden later, or above a
   ceiling set later, is not shown. Its row is kept, as viewing history is,
   in case the limit is lifted.
6. **On the title page and in the menus** (Chris's decision). A bookmark
   button beside Play, which is a shape, not a colour: gold means "where you
   are" and nothing else (`docs/design.md`). There is also an entry in More
   Options and in a tile's right-click menu.
7. **A Home shelf, "Watchlist",** shown when the list has anything on it. It
   can be turned off in Display & home, like the other shelves.

## Not decided here

The plan mentions letting friends' servers see your watchlist (a Privacy
setting). It is not built. A watchlist is as private as tags until somebody
asks for it to be shared, and presence (ADR 0045) is the place that would
decide how.

## Consequences

- **Schema 75** adds the table. It is a new table, which is why this ADR
  exists.
- **Routes:**
  - `GET /api/watchlist` lists the account's watchlist;
  - `PUT /api/items/{id}/watchlist` adds a title;
  - `DELETE /api/items/{id}/watchlist` removes it.

  All act on the session only and are not audited, like tags.
- **Cache keys.** Any change invalidates `["watchlist"]`. Finishing something
  changes the list too, so progress writes that mark an item watched
  invalidate it as well (CLAUDE.md: a write that changes what a list holds
  must invalidate that list).
