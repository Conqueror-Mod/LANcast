import { useParams, useSearchParams } from "react-router-dom";
import { useLibraries, useItems } from "@/api/hooks";
import { PosterTile } from "@/components/PosterTile";
import "./Browse.css";

export function Browse() {
  const { id } = useParams();
  const libraryID = Number(id);
  const [params, setParams] = useSearchParams();
  const q = params.get("q") ?? "";

  const { data: libraries } = useLibraries();
  const library = libraries?.find((l) => l.id === libraryID);
  const { data, isLoading, isError, error } = useItems({ libraryID, q });

  // Search state lives in the URL so any view is linkable and survives reload.
  const setQuery = (value: string) => {
    setParams(
      (prev) => {
        if (value) prev.set("q", value);
        else prev.delete("q");
        return prev;
      },
      { replace: true },
    );
  };

  const items = data?.items ?? [];

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
          onChange={(e) => setQuery(e.target.value)}
          aria-label="Search this library"
        />
      </div>

      {isError && (
        <p className="browse__message">
          {(error as Error)?.message ?? "Could not load this library."}
        </p>
      )}

      {!isError && items.length === 0 && !isLoading && (
        <p className="browse__message">
          {q ? `Nothing matches “${q}”.` : "This library is empty."}
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
