import type { ItemQuery } from "@/api/hooks";
import { FILTER_PARAM_KEYS } from "./browseFilters";
import { levelFor, type LibraryKindConfig } from "@/screens/libraryConfig";

/*
 * What a library's URL asks the server for: one definition, read by the
 * library grid and by a shelf pinned from it (ADR 0119 §2).
 *
 * A pinned shelf is a saved grid view, so the two must not be able to describe
 * different things. The grid used to build this object inline; a second copy
 * for Home would have drifted the first time a filter was added to one of
 * them.
 */

/*
 * Every parameter a pinned shelf keeps: the filters the bar owns, a person
 * from a cast link, the sort, the music level (`show`), the A-Z letter and a
 * search. Anything else in the URL is a view setting, not part of what is
 * listed. The server checks a pinned query against the same list
 * (internal/api/homeshelves.go).
 */
export const SHELF_PARAM_KEYS: readonly string[] = [
  ...FILTER_PARAM_KEYS,
  // Set by a cast link rather than the bar, and still what the grid lists.
  "person",
  "sort",
  "show",
  "initial",
  "q",
];

export function libraryItemQuery(
  libraryID: number,
  params: URLSearchParams,
  config: LibraryKindConfig,
): Omit<ItemQuery, "limit" | "offset"> {
  const q = params.get("q") ?? "";
  const level = levelFor(config, params.get("show"));
  const sorts = level?.sorts ?? config.sorts;
  // A sort this level does not offer is its default, rather than nothing.
  const asked = params.get("sort");
  const sort = sorts.some((s) => s.value === asked) ? asked! : sorts[0].value;
  const nums = (key: string) => params.getAll(key).map(Number);
  const initial = params.get("initial") ?? "";
  return {
    libraryID,
    q,
    sort,
    genres: params.getAll("genre"),
    decades: nums("decade"),
    contentRatings: params.getAll("content_rating"),
    unwatched: params.get("watched") === "false",
    years: nums("year"),
    resolutions: params.getAll("resolution"),
    platforms: params.getAll("platform"),
    releaseTypes: params.getAll("release_type"),
    people: nums("person"),
    actors: nums("actor"),
    directors: nums("director"),
    narrators: nums("narrator"),
    authors: params.getAll("author"),
    series: params.getAll("series"),
    collections: nums("collection"),
    faceClusters: nums("face_cluster"),
    // Your own tags (ADR 0062) and favourites. The bar drew their pills for
    // a release before they were sent, and the grid did not narrow.
    tags: nums("tag"),
    favourite: params.get("favourite") === "1",
    minRating: Number(params.get("min_rating") ?? 0),
    status: params.get("status") ?? "",
    /*
     * Containers that group items are not in the grid, except while
     * searching, where hiding a match would be the search lying. Must stay the
     * set the server's library count excludes (store.GroupingKinds).
     */
    excludeKind: q ? undefined : "collection,playlist",
    initial: initial || undefined,
    // Every album or track in the library rather than its top level.
    kind: level?.kind,
  };
}

/** Only the parameters a shelf keeps, in a stable order. */
export function shelfQuery(params: URLSearchParams): string {
  const out = new URLSearchParams();
  for (const key of SHELF_PARAM_KEYS) {
    for (const v of params.getAll(key)) if (v) out.append(key, v);
  }
  return out.toString();
}
