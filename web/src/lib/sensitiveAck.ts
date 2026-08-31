import { useSyncExternalStore } from "react";
import type { Item } from "@/api/types";

/*
 * Who has said "yes, show me" (ADR 0051).
 *
 * The server decides what is sensitive; this decides only whether the person
 * sitting here has asked to see it, and that is deliberately the one part of
 * the feature the server never learns. An acknowledgement is not interesting to
 * anyone but the person making it, and once it reaches the server it is in
 * backups and in the audit log for ever.
 *
 * It lasts the session and no longer. A permanent "yes I know" is easier to
 * build and defeats the point within a month: the folder stops being marked in
 * any way anybody notices, and the next time somebody else is in the room it is
 * just a folder. Session scope keeps the acknowledgement next to the act of
 * choosing to look.
 *
 * A module-level store rather than a context, so no provider has to be threaded
 * through App for a set of numbers, and so tests can reset it without mounting
 * anything.
 */
const acknowledged = new Set<number>();
const listeners = new Set<() => void>();

/*
 * A version counter, because the store is a mutable Set.
 *
 * useSyncExternalStore compares snapshots by identity, and a Set mutated in
 * place is identical to itself — so nothing would re-render. Returning a fresh
 * copy each time satisfies the comparison by never being equal, which
 * re-renders every tile on every render: the failure that looks like the
 * feature working. A number that only changes when the set does is neither.
 *
 * Bumped here rather than by a listener, so it cannot depend on the order
 * listeners happen to run in.
 */
let version = 0;

function announce() {
  version++;
  for (const l of listeners) l();
}

function subscribe(fn: () => void) {
  listeners.add(fn);
  return () => {
    listeners.delete(fn);
  };
}

/** acknowledge reveals one item, and anything the item is the parent of. */
export function acknowledge(id: number) {
  if (acknowledged.has(id)) return;
  acknowledged.add(id);
  announce();
}

/** forgetAcknowledgements is for tests, and for a sign-out. */
export function forgetAcknowledgements() {
  if (acknowledged.size === 0) return;
  acknowledged.clear();
  announce();
}

/*
 * revealed answers whether this item may be drawn.
 *
 * The parent is consulted as well as the item, and that is the whole of the
 * cascade. Entering a marked folder means acknowledging the folder, and having
 * done so, being asked again by each of the two hundred photographs inside it
 * would not be a privacy feature — it would be the reason somebody turns the
 * setting off.
 *
 * One level, not the whole ancestry, because one level is the structure that
 * exists: photographs hang off a gallery. A gallery nested inside an
 * acknowledged gallery is asked about on its own, which is right — it is a
 * separate thing somebody chose to mark, and you reach it by pressing it.
 *
 * Pure and exported so the rule can be tested without a store.
 */
export function revealed(item: Item, acked: ReadonlySet<number>): boolean {
  if (!item.sensitive) return true;
  if (acked.has(item.id)) return true;
  return item.parent_id != null && acked.has(item.parent_id);
}

/*
 * isRevealed applies the rule to what has actually been acknowledged.
 *
 * `revealed` takes the set so it can be tested as a rule; this is the same
 * question against the live store, and it is what a tile means.
 */
export function isRevealed(item: Item): boolean {
  return revealed(item, acknowledged);
}

/** obscured is the question a tile actually asks: should this be covered? */
export function useObscured(item: Item): boolean {
  useSyncExternalStore(
    subscribe,
    () => version,
    () => version,
  );
  return !isRevealed(item);
}
