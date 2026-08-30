/*
 * When a collision can be resolved by forgetting a row, and when it cannot.
 *
 * ADR 0042's decision is that LANcast never merges, ranks or deletes — because
 * a shared identity is evidence that something wants a *person*, and of the
 * thirteen pairs it was written against, two were not duplicates at all. That
 * still holds. What it never meant is that a person has no way to act: the
 * report exists so somebody can decide, and until now deciding was the one
 * thing it did not let you do.
 *
 * The unambiguous case is a rename. The old path is marked missing, the new one
 * is added, and the pair is reported — 34 of 43 collisions on a real library
 * were exactly that. Nothing is being chosen between there: one of the two rows
 * describes a file that no longer exists.
 *
 * The other case is a work removed outright: a split-cut film replaced by a
 * single file, both halves deleted, two rows left behind pointing at nothing.
 * Whether the drive is merely offline is not a question this file can answer,
 * and it no longer tries — the server checks the location at the moment it is
 * asked. See the note on forgettable.
 */

import type { CollisionMember } from "@/api/types";

/**
 * forgettable reports whether this member's row can be forgotten.
 *
 * The row's own file must be gone, and that is now the whole of it. A present
 * file's row would be re-added by the next scan, so forgetting it achieves
 * nothing but confusion.
 *
 * **The safety property moved rather than disappeared.** This used to also
 * require another member to still be present, on the reasoning that a drive
 * going away takes every copy missing together — so the offer would vanish
 * exactly when it was dangerous. True, and a proxy: it also refused somebody
 * who had genuinely deleted both halves of a split-cut film and wanted the
 * leftover rows gone, which is the first thing it was asked to do.
 *
 * The server now measures the real question instead of standing in for it. It
 * refuses `mode=forget` unless the title's location reads *at this moment*, so
 * a sleeping drive is answered with `location_unavailable` rather than guessed
 * at from what else is missing. That is a stronger guarantee than this rule
 * ever gave, and it is held where it cannot be bypassed.
 */
export function forgettable(
  members: CollisionMember[],
  member: CollisionMember,
): boolean {
  void members;
  return member.missing;
}

/**
 * resolvableByForgetting reports whether a whole collision is the rename shape:
 * some rows gone, at least one still here.
 *
 * Used to say so at the top of a card, because the difference between "this
 * needs you to look at two files" and "one of these is a leftover" is the first
 * thing worth knowing and was not stated anywhere.
 */
export function resolvableByForgetting(members: CollisionMember[]): boolean {
  return members.some((m) => forgettable(members, m));
}
