import type { Facets, CastMember } from "@/api/types";

/*
 * The browse filter model.
 *
 * Split out from the bar that draws it for the reason preroll.ts is split from
 * the player: the rule is the part worth testing and the wiring is the part
 * worth reading. What is worth testing here is the translation between a URL
 * and a set of readable pills, because that translation is where a filter
 * silently becomes a different filter.
 */

/** A filter category — one button in the bar, one panel when opened. */
export interface FilterCategory {
  /** The URL parameter this category writes. */
  key: string;
  label: string;
  /*
   * How the panel picks values.
   *
   * `chips` shows everything at once and is right up to a couple of dozen
   * values. `search` is for the facets that cannot be shown exhaustively — a
   * century of years, a library's worth of actors — where a wall of options is
   * not a control but a haystack.
   */
  mode: "chips" | "search";
  /** Single-valued categories replace rather than accumulate. */
  single?: boolean;
  /** Credit role this category searches within, for the two cast categories. */
  role?: string;
}

/*
 * The categories, in bar order.
 *
 * Genre first because it is what most people reach for; Status last because it
 * is library maintenance rather than browsing. Decade and Year sit together
 * deliberately — a decade is how you browse and a year is how you find, and
 * separating them across the bar would make the pair look like alternatives.
 */
export const FILTER_CATEGORIES: FilterCategory[] = [
  { key: "genre", label: "Genre", mode: "chips" },
  { key: "decade", label: "Decade", mode: "chips" },
  { key: "year", label: "Year", mode: "search" },
  /*
   * Two categories, not one "Cast".
   *
   * "Who is in this" and "who made this" are different questions, and an
   * any-role filter answers both without saying which was meant. Somebody
   * looking for films Eastwood directed does not want the ones he only acted
   * in. A person who does both simply appears under both, once in each.
   */
  { key: "actor", label: "Actor", mode: "search", role: "actor" },
  { key: "director", label: "Director", mode: "search", role: "director" },
  { key: "collection", label: "Collection", mode: "search" },
  // "Content rating" and "Rating" in full, because one is an age certificate
  // and the other is a score, and a bar with two buttons both reading "Rating"
  // is a bar nobody can use.
  { key: "content_rating", label: "Content rating", mode: "chips" },
  { key: "min_rating", label: "Rating", mode: "chips", single: true },
  { key: "resolution", label: "Format", mode: "chips" },
  { key: "status", label: "Status", mode: "chips", single: true },
];

/** Every parameter the filter bar owns. Used to clear them all at once without
 *  disturbing sort, search or the A–Z rail, which live in the same URL. */
export const FILTER_PARAM_KEYS = [
  ...FILTER_CATEGORIES.map((c) => c.key),
  "watched",
];

/** One active filter, as shown in the pill row. */
export interface FilterPill {
  key: string;
  value: string;
  label: string;
}

export interface PillContext {
  facets?: Facets;
  /** Names for the person ids currently filtered on. A pill whose name has not
   *  arrived yet is held back rather than shown as a raw id. */
  castNames?: Map<string, string>;
}

const STATUS_LABELS: Record<string, string> = {
  in_progress: "In progress",
  unmatched: "Unmatched",
};

/*
 * activePills turns the URL into the row of removable pills under the bar.
 *
 * This is the half Plex does not do: there, the active filters live inside the
 * dropdown that set them, so a grid filtered three ways looks exactly like an
 * unfiltered one until you go looking. Showing them costs a row and answers
 * "why am I seeing 41 of 1,190" without a click.
 *
 * A value whose label cannot be resolved yet — a person whose name is still in
 * flight — is omitted rather than rendered as an id. It appears a moment later
 * with a name, which is better than a pill that reads "person 12" and then
 * changes under the cursor.
 */
export function activePills(
  params: URLSearchParams,
  ctx: PillContext = {},
): FilterPill[] {
  const out: FilterPill[] = [];
  const push = (key: string, value: string, label: string) =>
    out.push({ key, value, label });

  for (const g of params.getAll("genre")) push("genre", g, g);
  for (const d of params.getAll("decade")) push("decade", d, `${d}s`);
  for (const y of params.getAll("year")) push("year", y, y);

  /*
   * Credit pills, held back until the name arrives.
   *
   * An id is not a name, and a pill reading "person 12" that becomes "Ada
   * Vance" under the cursor is worse than one that appears a moment late. The
   * role is on the pill because Ada acting and Ada directing are two different
   * filters that would otherwise be one indistinguishable word twice over.
   */
  for (const key of ["person", "actor", "director"]) {
    for (const id of params.getAll(key)) {
      const name = ctx.castNames?.get(id);
      if (!name) continue;
      push(key, id, key === "director" ? `${name} (director)` : name);
    }
  }

  for (const c of params.getAll("content_rating")) push("content_rating", c, c);

  for (const id of params.getAll("collection")) {
    // Named from the facets, which arrived with the page — so unlike a person,
    // a collection pill never has to wait for a second request.
    const col = ctx.facets?.collections?.find((c) => String(c.id) === id);
    if (col) push("collection", id, col.name);
  }

  const min = params.get("min_rating");
  if (min) push("min_rating", min, `${min}+`);

  for (const r of params.getAll("resolution")) {
    // The label comes from the server's bucket table rather than a copy here,
    // so a tier cannot be called "4K" in one place and "UHD" in another.
    const bucket = ctx.facets?.resolutions?.find((b) => b.key === r);
    push("resolution", r, bucket?.label ?? r);
  }

  const status = params.get("status");
  if (status && STATUS_LABELS[status]) {
    push("status", status, STATUS_LABELS[status]);
  }
  // The watched toggle is a filter like any other and belongs in the same row,
  // even though it is not one of the categories: a user does not care which
  // control set it, only that it is on.
  if (params.get("watched") === "false") push("watched", "false", "Unwatched");

  return out;
}

/** How many values a category currently has set — the number on its button. */
export function activeCount(params: URLSearchParams, key: string): number {
  // Single-valued categories are one or none however they are spelled.
  if (key === "status" || key === "min_rating") return params.get(key) ? 1 : 0;
  return params.getAll(key).length;
}

/*
 * The year list, narrowed by what has been typed.
 *
 * Years are searched on the client because the whole list is already here —
 * a hundred numbers arrive with the facets, and a round trip to filter them
 * would be slower than the typing. Cast cannot do this, which is the honest
 * difference between the two search panels.
 */
export function matchYears(years: number[], query: string): number[] {
  const q = query.trim();
  if (!q) return years;
  /*
   * Prefix, not substring.
   *
   * Substring looks more helpful and is not: "99" then matches 1994 as well as
   * 1999, because the digits are in there. Prefix makes typing narrow the way
   * a year is actually read — "19" is the century, "199" is the decade, "1994"
   * is the year — so every keystroke shortens the list instead of shuffling it.
   */
  return years.filter((y) => String(y).startsWith(q));
}

/** Cast rows as the panel lists them: name, and how much of the library they
 *  are in, which is what makes one Ada Vance distinguishable from another. */
export function castRowLabel(p: CastMember): string {
  return p.items === 1 ? "1 title" : `${p.items} titles`;
}

/*
 * The steps the Rating filter offers, mirroring store.RatingThresholds.
 *
 * Whole and half points down to 5: below that a rating filter stops separating
 * anything in a curated library, so the lower steps would all return the same
 * grid and read as broken controls.
 */
export const RATING_THRESHOLDS = [9, 8.5, 8, 7.5, 7, 6.5, 6, 5];

/*
 * The rating thresholds worth offering, given what the library actually holds.
 *
 * A step above the library's ceiling is a control guaranteed to return nothing,
 * which is the same "lies about what it does" failure the empty facets already
 * avoid. One step above the maximum is kept deliberately — a library topping
 * out at 8.4 still offers 8, which is the useful filter; it is 9 that goes.
 */
export function ratingSteps(thresholds: number[], maxRating: number): number[] {
  if (!maxRating) return [];
  return thresholds.filter((t) => t <= maxRating);
}

/** Collections narrowed by what has been typed, matched anywhere in the name —
 *  a franchise is as often remembered by its second word as its first. */
export function matchCollections<T extends { name: string }>(
  collections: T[],
  query: string,
): T[] {
  const q = query.trim().toLowerCase();
  if (!q) return collections;
  return collections.filter((c) => c.name.toLowerCase().includes(q));
}
