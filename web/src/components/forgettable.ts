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
 */

import type { CollisionMember } from "@/api/types";

/**
 * forgettable reports whether this member's row can be forgotten.
 *
 * Two conditions, and the second is the safety property rather than a nicety.
 *
 * The row's own file must be gone — a present file's row would be re-added by
 * the next scan, so forgetting it achieves nothing but confusion.
 *
 * And **another member must still be present**. "Scanning marks missing, never
 * deletes" exists so an unmounted drive cannot destroy library data, and a
 * button that forgets missing rows is exactly the hole that rule fears. When a
 * drive goes away every member of a collision goes missing together, so
 * requiring a surviving sibling means the offer disappears precisely when it
 * would be dangerous — and appears only when the work itself is safe, because
 * some other file still holds it.
 */
export function forgettable(
  members: CollisionMember[],
  member: CollisionMember,
): boolean {
  if (!member.missing) return false;
  return members.some((m) => m.id !== member.id && !m.missing);
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
