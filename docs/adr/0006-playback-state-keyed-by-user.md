# ADR 0006 — `playback_state` keyed by user from revision 1

Date: 2026-07-22 · Status: accepted

## Context

M1 is single-user. There is no authentication, no account model, and no
near-term plan for one — LANcast starts as a personal server on a trusted LAN.

The natural schema for that is `playback_state(item_id PRIMARY KEY,
position_ms, watched, updated_at)`. One row per item, no user concept, YAGNI
satisfied.

Multi-user is on the roadmap as a cross-cutting concern with no scheduled
milestone, which in practice means it might arrive in a year or never.

## Context that changes the calculus

Watch state is **irreplaceable user data**. Unlike a library row — which a
rescan regenerates in minutes — resume positions and watched flags exist
nowhere else. If they are lost, they are lost.

Adding a `user_id` column to a populated single-user table later means deciding
what to do with every existing row. The honest options are to assign them all to
a migrating user (wrong the moment two people were sharing the server, which is
the normal case for a household media server) or to drop them. Every media
server that has done this migration has annoyed its users.

## Decision

`playback_state` carries `user_id TEXT NOT NULL DEFAULT 'local'` from schema
revision 1, with `PRIMARY KEY (item_id, user_id)`.

M1 writes `'local'` for everything and never reads the column.

## Consequences

**Good.** Multi-user can arrive at any point with **zero data loss and no
migration decision**. Existing rows are simply the `local` user's history, and
that user can be adopted by the first real account.

**Good.** The cost today is one column with a default. Not one line of M1 logic
changes; the scanner, API, and client are all unaware of it.

**Good.** It documents intent in the place most likely to be read — the schema
itself — so the next person to touch this table knows multi-user is planned
before they build something that assumes otherwise.

**Cost.** A composite primary key where a simple one would do. Negligible.

**Cost.** It looks like speculative generality, and in isolation it is exactly
the thing YAGNI warns about. The distinction is that YAGNI applies cleanly to
*code*, which can be rewritten, and much less cleanly to *data*, which cannot.
Schema decisions affecting irreplaceable user data deserve a different threshold
than feature decisions.

## The general rule this establishes

Speculative flexibility in **code** is a cost. Speculative flexibility in
**schema columns that protect irreplaceable user data** is insurance, and it is
cheap. Apply the same reasoning to any future column of this shape.

The other place this rule already applies: `media_item.missing`, which exists so
a scan can mark absent files rather than delete rows — see
[ADR 0002](0002-one-wide-media-item-table.md) and `docs/architecture.md`.
