# ADR 0084 — A library can be kept from an account

Date: 2026-10-13 · Status: **accepted** 2026-10-13 · takes up the
"Revisit if" of [ADR 0015](0015-multi-user-accounts.md)

The plan is [settings-plan.md](../settings-plan.md), step 6. Chris decided on
2026-10-12 (§6, decision 4) that per-user library access is in scope, with
**every library visible to every account by default**: an administrator
narrows an account's libraries, and a new library is visible to everyone until
somebody narrows it.

## Context

ADR 0015 gave the server accounts and an admin/member split, and deferred
per-library visibility: "a genuinely larger design (an access-control list per
library)". The content-rating ceiling arrived later as the first per-account
limit, and is the precedent here. It is enforced at two chokepoints in the
store:

- **`MayPlay`**, reached through `GetItem`. Every handler that turns an id into
  an item comes through it (41 of them, including the detail page, playback,
  downloads, subtitles and saves), and a refusal is `ErrNotFound`, so "you may
  not see this" and "this does not exist" look the same from outside.
- **The listing predicate** in `ListItems`, plus `PermittedItems` for handlers
  that build their own SQL and hand back rows.

Auditing every route that returns library content for this ADR found that the
ceiling **does not reach the aggregate surfaces**:
- Continue Watching;
- Memories;
- Year in Review;
- viewing history;
- the People page and a library's cast;
- photo timelines, places and faces.

A restricted account cannot open anything those lists show, because `GetItem`
refuses it, but it can see that the titles exist. For a rating ceiling that
mostly shows what someone already watched. For a hidden library it would show
the library's contents, which is the thing being hidden.

## Decision

**One access rule per account, the ceiling and the hidden libraries together,
enforced at the same chokepoints and extended to every surface that lists
library content.**

1. **A deny list, not an allow list.** The new table is
   `library_hidden(user_id, library_id)`, keyed on both and cascading from
   both. A row hides one library from one account. No rows means everything is
   visible, so nothing changes for anybody until an administrator acts. A new
   library has no rows, so it is visible to everyone, which is the default
   Chris chose. An allow list would make every new library invisible to every
   member until someone remembered to grant it.
2. **Administrators see everything.** The rule applies to members only. An
   administrator can create and repoint libraries and could unhide one in a
   click, so hiding a library from one would protect nothing and only confuse.
   Any hidden rows an administrator holds are ignored, so promoting a member
   lifts their restrictions and demoting puts them back.
3. **The rule lives in the store, beside the ceiling.** An `access` value per
   account holds the ceiling plus the hidden library ids. It produces one SQL
   predicate, used by `ListItems`, `PermittedItems` and `MayPlay`. An account
   with no restriction gets an empty predicate and pays nothing.
4. **Refusal is a 404**, as for the ceiling. A library-scoped route such as
   `/api/libraries/{id}/…`, for a library the caller cannot see, answers
   exactly as for a library that does not exist.
5. **Every listing surface applies it**, including the ones the ceiling missed.
   Those gaps are closed for the ceiling too, since it is one rule. The surfaces:
   - **Libraries:** the library list itself, and every route under
     `/api/libraries/{id}/` (timeline, places, people, faces, duplicates, photo
     search, facets, cast, roots, scan).
   - **Lists:** browse, search, Home shelves, Continue Watching, a show's next
     episode, collections, playlists (members from a hidden library are left
     out, and the playlist itself is not deleted), Memories, Year in Review,
     viewing history, ratings, tags, and the People page.
   - **By id:** everything through `GetItem`, which already includes playback,
     streams, downloads, subtitles, lyrics, markers and saves.
6. **What a member sees in their own history stays theirs, but filtered.** A
   viewing of a film in a library later hidden is not deleted, because the
   history is the person's and the restriction can be lifted. It is left out of
   what they are shown while the library is hidden, so their history does not
   reveal what the library holds.
7. **Friends' servers are a separate question.** What a paired server may
   browse is decided by its share (ADR 0071), per library, already. An account
   restriction does not change what a friend sees, and a friend's share does
   not change what an account sees.

## What is not covered

- **Artwork by hash.** `/api/artwork/{hash}` serves images by content hash. A
  hash is learned only from a listing that returned it, so it cannot be guessed
  in practice, and reversing a hash into the items that use it on every image
  request would cost more than it protects. The ceiling made the same call.
- **Watch Together rooms** show a room's title to anybody on the server. Joining
  goes through `GetItem` and is refused. Filtering room titles is a small
  follow-up, not part of the first build.

## Consequences

- **Schema 74** adds the `library_hidden` table. It is a new table, not a
  column, which is why this ADR exists.
- **Admin routes:** `GET` and `PUT /api/users/{id}/libraries`. Hiding a
  library is audited, like setting a ceiling.
- **Users & access** gets a per-member library list with a switch per library.
- **It is built in stages, each mergeable alone**, with tests that a hidden
  library leaks nowhere on the surfaces that stage covers:
  1. The table, the rule at the chokepoints (`MayPlay`, `ListItems`,
     `PermittedItems`), the library list, the library-scoped routes, and the
     admin API.
  2. The aggregate surfaces: Continue Watching, Home shelves, Memories, Year in
     Review, history, people, cast, tags, search.
  3. The UI in Users & access.

  Until stage 3, a restriction can only be set through the API, so no
  household can be half-protected by a switch that does not yet hold
  everywhere.

## Alternatives rejected

**An allow list.** It leaves every new library invisible to every member until
someone grants it, the opposite of the decision.

**Hiding libraries only in the client.** It hides a menu entry and leaves every
route open. The API serves files, so a restriction that a hand-written request
gets round is not a restriction at all, which is the ceiling's founding lesson.

**A per-library "members may see" switch instead of per account.** It is
coarser in exactly the case that matters: a household where one person's
library is private from one other person, not from everybody.
