import { useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useInfiniteItems, useFacets, useRecentPhotos } from "@/api/hooks";
import { PosterTile } from "@/components/PosterTile";
import { PhotoBanner } from "@/components/PhotoBanner";
import { PhotoViewer } from "@/components/PhotoViewer";
import { FilterChips } from "@/components/FilterChips";
import type { Item, Library } from "@/api/types";
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
  // The default comes from the kind rather than being "title" for everyone: a
  // picture library sorted by title is sorted by UUID, which is no order at
  // all. Each config lists its natural default first.
  const sort = params.get("sort") ?? config.sorts[0].value;
  const genres = params.getAll("genre");
  const decades = params.getAll("decade");
  const contentRatings = params.getAll("content_rating");
  const unwatched = params.get("watched") === "false";

  const { data: facets } = useFacets(libraryID);
  const {
    data,
    isLoading,
    isError,
    error,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useInfiniteItems({
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

  // A sentinel below the grid pulls the next page into view as the user reaches
  // it, so a large library scrolls continuously instead of stopping at the first
  // page with no sign there is more.
  const sentinel = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    if (!hasNextPage || isFetchingNextPage) return;
    const el = sentinel.current;
    if (!el) return;

    // Near enough to the viewport that the next page should already be loading.
    const near = () =>
      el.getBoundingClientRect().top < window.innerHeight + 600;
    const maybeFetch = () => {
      if (near()) fetchNextPage();
    };

    const io = new IntersectionObserver(
      (entries) => {
        if (entries[0]?.isIntersecting) fetchNextPage();
      },
      { rootMargin: "600px" },
    );
    io.observe(el);

    // Scroll and resize back the observer up. Observer callbacks are suppressed
    // in a hidden or throttled tab, and if they never arrive the grid just stops
    // — a silent truncation with no sign there is more, which is the failure
    // this paging exists to remove. The immediate call also covers a first page
    // too short to fill the viewport.
    window.addEventListener("scroll", maybeFetch, { passive: true });
    window.addEventListener("resize", maybeFetch);
    maybeFetch();

    return () => {
      io.disconnect();
      window.removeEventListener("scroll", maybeFetch);
      window.removeEventListener("resize", maybeFetch);
    };
  }, [hasNextPage, isFetchingNextPage, fetchNextPage]);

  // A picture library opens on a banner cycling the library itself (ADR 0028).
  // Drawn from the newest photographs rather than a random sample, because
  // "random" over 2,610 files means a fresh trip to the database on every visit
  // and a banner that can show the same picture twice in a row; the newest are
  // already cached for the Home row, and shuffling them client-side gives the
  // variety without the cost.
  const isPictures = library.kind === "picture";
  const { data: bannerPool } = useRecentPhotos(isPictures ? 24 : 0);
  const [shownPhoto, setShownPhoto] = useState<Item | null>(null);
  const [viewerOpen, setViewerOpen] = useState(false);
  const bannerPhotos = useMemo(() => shuffle(bannerPool ?? []), [bannerPool]);

  const items = data?.pages.flatMap((p) => p.items) ?? [];
  const total = data?.pages[0]?.total ?? 0;
  const filtered =
    !!q ||
    genres.length > 0 ||
    decades.length > 0 ||
    contentRatings.length > 0 ||
    unwatched;

  return (
    <div className="browse">
      {isPictures && bannerPhotos.length > 0 && (
        <PhotoBanner
          photos={bannerPhotos}
          selected={shownPhoto}
          label={library.name}
          onExpand={(p) => {
            setShownPhoto(p);
            setViewerOpen(true);
          }}
        />
      )}
      {viewerOpen && (
        <PhotoViewer
          photos={bannerPhotos}
          startAt={Math.max(
            0,
            bannerPhotos.findIndex((p) => p.id === shownPhoto?.id),
          )}
          onClose={() => setViewerOpen(false)}
        />
      )}
      <div className="browse__header">
        <div className="browse__heading">
          <span className="section-label">{library.name}</span>
          <span className="browse__rule" />
          {data && (
            <span className="browse__count">
              {items.length < total
                ? `${items.length.toLocaleString()} of ${total.toLocaleString()}`
                : total.toLocaleString()}
            </span>
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
          <select
            value={sort}
            onChange={(e) => setParam("sort", e.target.value)}
          >
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
          {filtered
            ? "Nothing matches these filters."
            : "This library is empty."}
        </p>
      )}

      <div className="browse__grid">
        {items.map((item) => (
          <PosterTile key={item.id} item={item} />
        ))}
      </div>

      {/* The sentinel needs real height: a zero-area element never reports as
          intersecting, so an unsized marker silently never triggers the next
          page. It doubles as the "loading more" strip. */}
      {hasNextPage && (
        <div ref={sentinel} className="browse__more">
          {isFetchingNextPage ? "Loading more…" : ""}
        </div>
      )}
    </div>
  );
}

// A stable shuffle for one render of one pool. Seeded by nothing and computed
// once per pool via useMemo: re-shuffling on every render would reorder the
// banner mid-cycle, which looks like a bug rather than variety.
function shuffle(items: Item[]): Item[] {
  const out = items.slice();
  for (let i = out.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1));
    [out[i], out[j]] = [out[j], out[i]];
  }
  return out;
}
