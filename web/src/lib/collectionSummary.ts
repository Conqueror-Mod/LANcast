import type { CollectionFilm, Item } from "@/api/types";
import { runtime } from "./format";

/*
 * The facts a collection's page states under its title.
 *
 * A collection has no year, runtime or certificate of its own, so the line
 * that says what a film is said only "5 films" for a box set. Each piece here
 * is derived from what is on screen beneath it — the members, and the
 * provider's list — so the line cannot disagree with the page.
 *
 *   years    the span of the members' years: "2001–2003", or one year
 *   runtime  the members' running times added up: "9h 18m"
 *   watched  "2 of 5 watched", "All watched", "Started" with only a film part
 *            way, or nothing when untouched
 *   owned    "You have 3 of 5", only when the provider lists films not here
 */
export function collectionSummary(
  item: Item,
  members: Item[],
  films: CollectionFilm[] | undefined,
): { years: string; runtime: string; watched: string; owned: string } {
  const years = members.map((m) => m.year).filter((y): y is number => !!y);
  const lo = years.length ? Math.min(...years) : 0;
  const hi = years.length ? Math.max(...years) : 0;
  const span = !lo ? "" : lo === hi ? String(lo) : `${lo}–${hi}`;

  const total = members.reduce((n, m) => n + (m.duration_ms ?? 0), 0);

  const p = item.collection_progress;
  const watched = !p || p.total <= 0
    ? ""
    : p.watched >= p.total
      ? "All watched"
      : p.watched > 0
        ? `${p.watched} of ${p.total} watched`
        : p.in_progress > 0
          ? "Started"
          : "";

  /*
   * Released films only, on both sides of the count. An announced sequel is
   * in the provider's list, and "you have 3 of 4" when the fourth does not
   * exist yet would read as a gap in somebody's shelf.
   */
  const released = (films ?? []).filter((f) => !f.upcoming);
  const have = released.filter((f) => f.item_id).length;
  const owned = released.length > 0 && have < released.length
    ? `You have ${have} of ${released.length}`
    : "";

  return { years: span, runtime: runtime(total), watched, owned };
}

// The films the provider lists that are not in the library, in release order.
export function missingFilms(films: CollectionFilm[] | undefined): CollectionFilm[] {
  return (films ?? []).filter((f) => !f.item_id);
}
