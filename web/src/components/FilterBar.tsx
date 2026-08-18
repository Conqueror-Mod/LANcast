import { useEffect, useRef, useState } from "react";
import type { Facets } from "@/api/types";
import { useCast } from "@/api/hooks";
import {
  FILTER_CATEGORIES,
  activeCount,
  activePills,
  castRowLabel,
  matchCollections,
  matchYears,
  ratingSteps,
  type FilterCategory,
} from "@/lib/browseFilters";
import { RATING_THRESHOLDS } from "@/lib/browseFilters";
import "./FilterBar.css";

/*
 * The browse filter bar.
 *
 * Categories as buttons, one open at a time, and every active value repeated as
 * a removable pill underneath. The pill row is the point: filter state lives in
 * the URL and used to be legible only by reading the chips that set it, so a
 * grid narrowed three ways looked like an unfiltered one showing suspiciously
 * little. A row of pills answers "why am I seeing 41 of 1,190" without opening
 * anything, which is the half Plex leaves out — there the active filters live
 * inside the dropdown that set them.
 *
 * A panel rather than a permanent row of every value, because the facets differ
 * by three orders of magnitude: a dozen genres can all be on screen, a century
 * of years and a library of actors cannot. Categories declare which they are
 * and the panel obeys.
 */
export function FilterBar({
  libraryID,
  facets,
  params,
  castNames,
  onToggle,
  onSet,
  onClear,
}: {
  libraryID: number;
  facets?: Facets;
  params: URLSearchParams;
  /** Names for the person ids in the URL, so a bookmarked filter renders as a
   *  name rather than as "person 12". */
  castNames?: Map<string, string>;
  onToggle: (key: string, value: string) => void;
  onSet: (key: string, value: string) => void;
  onClear: () => void;
}) {
  const [open, setOpen] = useState<string | null>(null);
  const wrap = useRef<HTMLDivElement>(null);

  /*
   * Close on Escape or on a click outside.
   *
   * Both listeners go on `document`, not `window`: this app resolves Escape
   * centrally on `document`, and a window listener would lose that race and
   * never fire — the trap the player already hit with fullscreen.
   */
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (wrap.current && !wrap.current.contains(e.target as Node)) setOpen(null);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(null);
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  /*
   * A category with nothing to offer is not drawn.
   *
   * The rule the chips already followed: a control that cannot change the grid
   * lies about what it does. Cast is the exception and is always offered,
   * because it is a search rather than a facet — the bar cannot know whether
   * anybody is credited without asking, and an empty panel says so in words.
   */
  const has = (c: FilterCategory): boolean => {
    switch (c.key) {
      case "genre":
        return (facets?.genres?.length ?? 0) > 0;
      case "decade":
        return (facets?.decades?.length ?? 0) > 0;
      case "year":
        return (facets?.years?.length ?? 0) > 0;
      case "content_rating":
        return (facets?.content_ratings?.length ?? 0) > 0;
      case "collection":
        return (facets?.collections?.length ?? 0) > 0;
      case "min_rating":
        return ratingSteps(RATING_THRESHOLDS, facets?.max_rating ?? 0).length > 0;
      case "resolution":
        return (facets?.resolutions?.length ?? 0) > 0;
      case "status":
        return !!(
          facets?.has_in_progress ||
          facets?.has_unmatched ||
          facets?.has_watched
        );
      case "actor":
      case "director":
        return true;
      default:
        return false;
    }
  };

  const pills = activePills(params, { facets, castNames });

  return (
    <div className="fbar" ref={wrap}>
      <div className="fbar__cats">
        {FILTER_CATEGORIES.filter(has).map((c) => {
          const n = activeCount(params, c.key);
          const isOpen = open === c.key;
          return (
            <button
              key={c.key}
              type="button"
              className={"fbar__cat" + (isOpen || n > 0 ? " is-on" : "")}
              aria-expanded={isOpen}
              aria-haspopup="dialog"
              onClick={() => setOpen(isOpen ? null : c.key)}
            >
              {c.label}
              {n > 0 && <span className="fbar__count">{n}</span>}
            </button>
          );
        })}
      </div>

      {open && (
        <FilterPanel
          category={FILTER_CATEGORIES.find((c) => c.key === open)!}
          libraryID={libraryID}
          facets={facets}
          params={params}
          onToggle={onToggle}
          onSet={onSet}
        />
      )}

      {pills.length > 0 && (
        <div className="fbar__pills">
          {pills.map((p) => (
            <button
              key={p.key + p.value}
              type="button"
              className="fbar__pill"
              // The pill *is* the remove control, so it says so: "Drama" read
              // out alone would not tell a screen-reader user that pressing it
              // takes the filter off.
              aria-label={`Remove filter ${p.label}`}
              onClick={() =>
                p.key === "status" || p.key === "watched"
                  ? onSet(p.key, "")
                  : onToggle(p.key, p.value)
              }
            >
              {p.label}
              <span aria-hidden="true" className="fbar__pillx">
                ×
              </span>
            </button>
          ))}
          {pills.length > 1 && (
            <button type="button" className="fbar__clear" onClick={onClear}>
              Clear all
            </button>
          )}
        </div>
      )}
    </div>
  );
}

// The open category's contents.
function FilterPanel({
  category,
  libraryID,
  facets,
  params,
  onToggle,
  onSet,
}: {
  category: FilterCategory;
  libraryID: number;
  facets?: Facets;
  params: URLSearchParams;
  onToggle: (key: string, value: string) => void;
  onSet: (key: string, value: string) => void;
}) {
  const [query, setQuery] = useState("");
  const selected = new Set(params.getAll(category.key));

  const chip = (value: string, label: string, on: boolean, set: () => void) => (
    <button
      key={value}
      type="button"
      className={"chip" + (on ? " is-on" : "")}
      aria-pressed={on}
      onClick={set}
    >
      {label}
    </button>
  );

  return (
    <div className="fbar__panel" role="dialog" aria-label={category.label}>
      {category.mode === "search" && (
        <input
          className="fbar__search"
          autoFocus
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={`Search ${category.label.toLowerCase()}`}
          aria-label={`Search ${category.label}`}
        />
      )}

      <div className="fbar__opts">
        {category.key === "genre" &&
          (facets?.genres ?? []).map((g) =>
            chip(g, g, selected.has(g), () => onToggle("genre", g)),
          )}

        {category.key === "decade" &&
          (facets?.decades ?? []).map((d) =>
            chip(String(d), `${d}s`, selected.has(String(d)), () =>
              onToggle("decade", String(d)),
            ),
          )}

        {category.key === "content_rating" &&
          (facets?.content_ratings ?? []).map((c) =>
            chip(c, c, selected.has(c), () => onToggle("content_rating", c)),
          )}

        {category.key === "resolution" &&
          (facets?.resolutions ?? []).map((b) =>
            chip(b.key, b.label, selected.has(b.key), () =>
              onToggle("resolution", b.key),
            ),
          )}

        {category.key === "year" &&
          matchYears(facets?.years ?? [], query).map((y) =>
            chip(String(y), String(y), selected.has(String(y)), () =>
              onToggle("year", String(y)),
            ),
          )}

        {category.key === "collection" &&
          matchCollections(facets?.collections ?? [], query).map((c) =>
            chip(
              String(c.id),
              // The member count separates a franchise from a two-film pairing,
              // which is the whole question when picking one from a list.
              `${c.name} (${c.members})`,
              selected.has(String(c.id)),
              () => onToggle("collection", String(c.id)),
            ),
          )}

        {category.key === "min_rating" &&
          ratingSteps(RATING_THRESHOLDS, facets?.max_rating ?? 0).map((t) => {
            const on = params.get("min_rating") === String(t);
            // Single-valued: picking a threshold replaces the last one, and
            // picking the one already on clears it.
            return chip(String(t), `${t}+`, on, () =>
              onSet("min_rating", on ? "" : String(t)),
            );
          })}

        {category.key === "status" && (
          <StatusOptions params={params} facets={facets} onSet={onSet} />
        )}

        {(category.key === "actor" || category.key === "director") && (
          <CastOptions
            libraryID={libraryID}
            query={query}
            role={category.role ?? ""}
            paramKey={category.key}
            selected={selected}
            onToggle={onToggle}
          />
        )}
      </div>
    </div>
  );
}

/*
 * Status is single-valued, and it is where the watched toggle now lives.
 *
 * Unwatched used to sit alone as a chip while In progress and Unmatched had
 * nowhere to go; grouping the three is most of why this bar exists. Clicking
 * the one already on turns it off, so the group needs no separate "Any".
 */
function StatusOptions({
  params,
  facets,
  onSet,
}: {
  params: URLSearchParams;
  facets?: Facets;
  onSet: (key: string, value: string) => void;
}) {
  const status = params.get("status") ?? "";
  const unwatched = params.get("watched") === "false";
  const opts = [
    {
      key: "unwatched",
      label: "Unwatched",
      on: unwatched,
      set: () => onSet("watched", unwatched ? "" : "false"),
      show: !!facets?.has_watched,
    },
    {
      key: "in_progress",
      label: "In progress",
      on: status === "in_progress",
      set: () => onSet("status", status === "in_progress" ? "" : "in_progress"),
      show: !!facets?.has_in_progress,
    },
    {
      key: "unmatched",
      label: "Unmatched",
      on: status === "unmatched",
      set: () => onSet("status", status === "unmatched" ? "" : "unmatched"),
      show: !!facets?.has_unmatched,
    },
  ];
  return (
    <>
      {opts
        .filter((o) => o.show)
        .map((o) => (
          <button
            key={o.key}
            type="button"
            className={"chip" + (o.on ? " is-on" : "")}
            aria-pressed={o.on}
            onClick={o.set}
          >
            {o.label}
          </button>
        ))}
    </>
  );
}

// The cast type-ahead — the only panel whose contents come from the network.
function CastOptions({
  libraryID,
  query,
  role,
  paramKey,
  selected,
  onToggle,
}: {
  libraryID: number;
  query: string;
  role: string;
  paramKey: string;
  selected: Set<string>;
  onToggle: (key: string, value: string) => void;
}) {
  const cast = useCast(libraryID, query, role);
  const people = cast.data?.people ?? [];

  if (cast.isLoading && people.length === 0) {
    return <p className="fbar__note">Searching…</p>;
  }
  if (people.length === 0) {
    /*
     * Two different absences, said differently. A library with no credits at
     * all is not the same as a search that found nobody, and telling them apart
     * is the difference between "your library needs metadata" and "try another
     * spelling".
     */
    return (
      <p className="fbar__note">
        {query
          ? `Nobody here matching ${query}.`
          : `No ${role}s recorded yet — this library needs metadata.`}
      </p>
    );
  }
  return (
    <>
      {people.map((p) => {
        const on = selected.has(String(p.id));
        return (
          <button
            key={p.id}
            type="button"
            className={"fbar__person" + (on ? " is-on" : "")}
            aria-pressed={on}
            onClick={() => onToggle(paramKey, String(p.id))}
          >
            <span className="fbar__personname">{p.name}</span>
            {/* How much of the library they are in — what makes one row worth
                picking over another when a search returns four Smiths. */}
            <span className="fbar__personcount">{castRowLabel(p)}</span>
          </button>
        );
      })}
    </>
  );
}
