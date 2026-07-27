import { useParams, useSearchParams } from "react-router-dom";
import { useLibraries, useItems, useFacets } from "@/api/hooks";
import { PosterTile } from "@/components/PosterTile";
import "./Browse.css";

const SORTS: { value: string; label: string }[] = [
  { value: "title", label: "Title" },
  { value: "year", label: "Year" },
  { value: "added", label: "Recently added" },
];

export function Browse() {
  const { id } = useParams();
  const libraryID = Number(id);
  const [params, setParams] = useSearchParams();
  const q = params.get("q") ?? "";
  const sort = params.get("sort") ?? "title";
  const genre = params.get("genre") ?? "";
  const decade = params.get("decade") ?? "";

  const { data: libraries } = useLibraries();
  const library = libraries?.find((l) => l.id === libraryID);
  const { data: facets } = useFacets(libraryID);
  const { data, isLoading, isError, error } = useItems({
    libraryID,
    q,
    sort,
    genre: genre || undefined,
    decade: decade ? Number(decade) : undefined,
  });

  // Every control lives in the URL, so a filtered view is linkable and survives
  // reload. An empty value clears the key.
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

  const items = data?.items ?? [];
  const filtered = q || genre || decade;

  return (
    <div className="browse">
      <div className="browse__header">
        <div className="browse__heading">
          <span className="section-label">{library?.name ?? "Library"}</span>
          <span className="browse__rule" />
          {data && (
            <span className="browse__count">{data.total.toLocaleString()}</span>
          )}
        </div>
        <input
          className="browse__search"
          type="search"
          placeholder="Search this library"
          value={q}
          onChange={(e) => setParam("q", e.target.value)}
          aria-label="Search this library"
        />
      </div>

      <div className="browse__filters">
        <label className="browse__filter">
          <span>Sort</span>
          <select value={sort} onChange={(e) => setParam("sort", e.target.value)}>
            {SORTS.map((s) => (
              <option key={s.value} value={s.value}>
                {s.label}
              </option>
            ))}
          </select>
        </label>

        {facets && facets.genres.length > 0 && (
          <label className="browse__filter">
            <span>Genre</span>
            <select
              value={genre}
              onChange={(e) => setParam("genre", e.target.value)}
            >
              <option value="">All</option>
              {facets.genres.map((g) => (
                <option key={g} value={g}>
                  {g}
                </option>
              ))}
            </select>
          </label>
        )}

        {facets && facets.decades.length > 0 && (
          <label className="browse__filter">
            <span>Decade</span>
            <select
              value={decade}
              onChange={(e) => setParam("decade", e.target.value)}
            >
              <option value="">All</option>
              {facets.decades.map((d) => (
                <option key={d} value={String(d)}>
                  {d}s
                </option>
              ))}
            </select>
          </label>
        )}

        {filtered && (
          <button
            className="browse__clear"
            onClick={() => {
              setParams(
                (prev) => {
                  for (const k of ["q", "genre", "decade"]) prev.delete(k);
                  return prev;
                },
                { replace: true },
              );
            }}
          >
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
