import { useSearchParams } from "react-router-dom";
import { useItems, useFacets } from "@/api/hooks";
import { PosterTile } from "@/components/PosterTile";
import { FilterChips } from "@/components/FilterChips";
import type { Library } from "@/api/types";
import type { LibraryKindConfig } from "./libraryConfig";
import "./Browse.css";

// The facet params that live in the URL as repeated keys. Search, sort, and the
// watched toggle are single-valued and handled separately.
const FACET_KEYS = ["genre", "decade", "content_rating"] as const;

// The shared browse shell — header, count, search, filter row, and grid. The
// per-kind differences (sort options, copy) come in as `config`, so a movie
// library and a TV library are the same component with different configuration
// rather than two divergent screens.
export function LibraryView({
  library,
  config,
}: {
  library: Library;
  config: LibraryKindConfig;
}) {
  const libraryID = library.id;
  const [params, setParams] = useSearchParams();
  const q = params.get("q") ?? "";
  const sort = params.get("sort") ?? "title";
  const genres = params.getAll("genre");
  const decades = params.getAll("decade");
  const contentRatings = params.getAll("content_rating");
  const unwatched = params.get("watched") === "false";

  const { data: facets } = useFacets(libraryID);
  const { data, isLoading, isError, error } = useItems({
    libraryID,
    q,
    sort,
    genres,
    decades: decades.map(Number),
    contentRatings,
    unwatched,
  });

  // Every control lives in the URL, so a filtered view is linkable and survives
  // reload. Single-valued keys set/clear; an empty value removes the key.
  const setParam = (key: string, value: string) => {
    setParams(
      (prev) => {
        if (value) prev.set(key, value);
        else prev.delete(key);
        return prev;
      },
      { replace: true },
    );
  };

  // Multi-valued facets: toggle one value in a repeated key without disturbing
  // the others.
  const toggleParam = (key: string, value: string) => {
    setParams(
      (prev) => {
        const cur = prev.getAll(key);
        prev.delete(key);
        const next = cur.includes(value)
          ? cur.filter((v) => v !== value)
          : [...cur, value];
        for (const v of next) prev.append(key, v);
        return prev;
      },
      { replace: true },
    );
  };

  const clearFilters = () => {
    setParams(
      (prev) => {
        for (const k of [...FACET_KEYS, "q", "watched"]) prev.delete(k);
        return prev;
      },
      { replace: true },
    );
  };

  const items = data?.items ?? [];
  const filtered =
    !!q ||
    genres.length > 0 ||
    decades.length > 0 ||
    contentRatings.length > 0 ||
    unwatched;

  return (
    <div className="browse">
      <div className="browse__header">
        <div className="browse__heading">
          <span className="section-label">{library.name}</span>
          <span className="browse__rule" />
          {data && (
            <span className="browse__count">{data.total.toLocaleString()}</span>
          )}
        </div>
        <input
          className="browse__search"
          type="search"
          placeholder={config.searchPlaceholder}
          value={q}
          onChange={(e) => setParam("q", e.target.value)}
          aria-label={config.searchPlaceholder}
        />
      </div>

      <div className="browse__filters">
        <label className="browse__filter">
          <span>Sort</span>
          <select value={sort} onChange={(e) => setParam("sort", e.target.value)}>
            {config.sorts.map((s) => (
              <option key={s.value} value={s.value}>
                {s.label}
              </option>
            ))}
          </select>
        </label>

        <FilterChips
          label="Genre"
          options={(facets?.genres ?? []).map((g) => ({ value: g, label: g }))}
          selected={new Set(genres)}
          onToggle={(v) => toggleParam("genre", v)}
        />

        <FilterChips
          label="Decade"
          options={(facets?.decades ?? []).map((d) => ({
            value: String(d),
            label: `${d}s`,
          }))}
          selected={new Set(decades)}
          onToggle={(v) => toggleParam("decade", v)}
        />

        <FilterChips
          label="Rating"
          options={(facets?.content_ratings ?? []).map((c) => ({
            value: c,
            label: c,
          }))}
          selected={new Set(contentRatings)}
          onToggle={(v) => toggleParam("content_rating", v)}
        />

        {facets?.has_watched && (
          <div className="chips">
            <span className="chips__label">Status</span>
            <div className="chips__row">
              <button
                type="button"
                className={"chip" + (unwatched ? " is-on" : "")}
                aria-pressed={unwatched}
                onClick={() => setParam("watched", unwatched ? "" : "false")}
              >
                Unwatched
              </button>
            </div>
          </div>
        )}

        {filtered && (
          <button className="browse__clear" onClick={clearFilters}>
            Clear
          </button>
        )}
      </div>

      {isError && (
        <p className="browse__message">
          {(error as Error)?.message ?? "Could not load this library."}
        </p>
      )}

      {!isError && items.length === 0 && !isLoading && (
        <p className="browse__message">
          {filtered ? "Nothing matches these filters." : "This library is empty."}
        </p>
      )}

      <div className="browse__grid">
        {items.map((item) => (
          <PosterTile key={item.id} item={item} />
        ))}
      </div>
    </div>
  );
}
