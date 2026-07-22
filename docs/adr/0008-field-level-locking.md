# ADR 0008 — Field-level locking

Date: 2026-07-22 · Status: accepted

## Context

Automatic matching will sometimes be wrong. This is not a solvable problem — it
is a permanent property of matching fuzzy filenames against a database of
similarly-named titles.

What *is* solvable is what happens next. Every media server in this space
produces the same story: a film is misidentified, the user corrects it, and some
later rescan or metadata refresh silently reverts the correction. It is a small
bug with an outsized effect, because it teaches users that **correcting things
does not work** — after which they stop trying, and the library degrades
permanently.

Metadata for any given field can come from four places, and they disagree: the
user's manual edit, an NFO sidecar, the provider, and the filename guess.

Options considered: provider always wins; NFO always wins; item-level locking;
field-level locking.

## Decision

**Field-level locking**, with precedence resolved independently per field:

> user lock → NFO → provider → filename guess

```sql
CREATE TABLE item_lock (
    item_id INTEGER NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    field   TEXT    NOT NULL,
    PRIMARY KEY (item_id, field)
);
```

Editing a field creates a lock on exactly that field. The merge engine skips
locked fields on every subsequent refresh. Separately, a corrected *match* sets
`match_state = 'locked'`, and locked items are never re-scored or re-searched.

## Consequences

**Good.** A correction costs exactly the field it touched. Fix a wrong title and
the item keeps receiving artwork, cast, ratings, and new episode data. Under
item-level locking that same one-word fix silently opts the item out of all
future metadata — a cost wildly disproportionate to the action, and one the user
has no way to anticipate.

**Good.** Corrections become *safe*, which is what makes them something users
will actually do. A system where fixing things is cheap and durable gets fixed;
one where it is expensive or unreliable gets abandoned.

**Good.** The precedence chain is the same for every field, so behavior is
predictable. There is no field with special rules to remember.

**Cost.** More bookkeeping than a single boolean per item. One extra table and a
merge engine that consults it. Contained, and paid once.

**Cost.** The UI now has an obligation: **locked fields must be visible and
individually releasable.** A lock the user cannot see or undo is
indistinguishable from a bug — it produces exactly the "why won't this update?"
confusion this ADR exists to prevent. Field-level locking without lock
indicators is worse than item-level locking.

**Cost.** Users must understand that editing implies locking. Mitigated by
showing the lock indicator immediately on edit, so the cause and effect are
visible at the moment they happen rather than discovered weeks later.

## Verification

This behavior gets a permanent integration test, not a one-time manual check:

> Match an item, correct its title, rescan the library, refresh metadata.
> The correction survives. Unlocked fields still update.

If that test ever fails, LANcast has become the thing it was built to replace.
