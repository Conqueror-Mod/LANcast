/*
 * The ceilings offered, in order, and there is exactly one list.
 *
 * A short list rather than every label internal/rating can place. The full
 * table carries six national systems so that *items* from any of them can be
 * judged; offering all of them would ask a household to choose between "15"
 * and "TV-14" as though the difference meant something to them. These are the
 * rungs somebody actually thinks in, and an item rated in another system is
 * still placed against whichever one is chosen.
 *
 * Shared rather than duplicated because
 * [ADR 0071](../../../docs/adr/0071-a-shared-library-is-a-standing-grant.md)
 * §6 says a share's limit comes from "the same rungs an account ceiling
 * offers". Two lists that could drift apart would make that sentence quietly
 * false — a household would set "PG-13" for a friend and "PG-13" for a child
 * and have no reason to expect the two to mean different things.
 */
export const RATING_RUNGS = ["G", "PG", "PG-13", "TV-14", "R"];

/*
 * The certificates offered when rating an item by hand, by what it is.
 *
 * Every one is a label internal/rating places exactly as written — the server
 * refuses anything else, because a rating the ceiling query cannot match
 * would look set and go on hiding the item. A game gets ESRB with its system's
 * name ("ESRB M" is 17+; a bare "M" is Australia's 15), television the TV
 * ratings, and a film the US film certificates.
 *
 * KA is offered, beside E. It is what ESRB called E from 1994 until 1998, so
 * it is the mark actually printed on the box of most rated SNES, Genesis and
 * early PlayStation games — Donkey Kong Country says K-A, not E. Leaving it
 * out made the list right only for a game the DAT had already rated KA.
 */
const ESRB = ["ESRB EC", "ESRB KA", "ESRB E", "ESRB E10+", "ESRB T", "ESRB M", "ESRB AO"];
const TV = ["TV-Y", "TV-Y7", "TV-G", "TV-PG", "TV-14", "TV-MA"];
const FILM = ["G", "PG", "PG-13", "R", "NC-17"];

export function ratingChoices(kind: string, current?: string | null): string[] {
  const base =
    kind === "rom" ? ESRB : kind === "show" || kind === "season" || kind === "episode" ? TV : FILM;
  return current && !base.includes(current) ? [current, ...base] : base;
}

/** Kinds no certificate describes, which are never rated (store/ceiling.go). */
const UNRATABLE = new Set(["artist", "album", "track", "playlist", "gallery", "photo", "collection"]);

export function canBeRated(kind: string): boolean {
  return !UNRATABLE.has(kind);
}
