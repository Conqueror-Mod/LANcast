import { useMemo, useRef, useState, type CSSProperties } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import {
  useInfiniteItems,
  useFacets,
  useCastByIDs,
  useFacePeople,
  useRecentPhotos,
  fetchLibraryTracks,
  playableKindFor,
  type PlayableKind,
} from "@/api/hooks";
import { PosterTile } from "@/components/PosterTile";
import { SensitiveReveal } from "@/lib/sensitiveAck";
import { useItemActions } from "@/components/itemActions";
import { startOf } from "@/playback/queueOrder";
import { PhotoBanner } from "@/components/PhotoBanner";
import { PhotoViewer } from "@/components/PhotoViewer";
import { FilterBar } from "@/components/FilterBar";
import { AlphabetRail } from "@/components/AlphabetRail";
import { TileSizeSlider } from "@/components/TileSizeSlider";
import { useInfiniteScroll } from "@/lib/useInfiniteScroll";
import { tileWidth, useTileSize } from "@/lib/tileSize";
import { FILTER_PARAM_KEYS } from "@/lib/browseFilters";
import { ShuffleGlyph } from "@/components/PlayerGlyphs";
import type { Item, Library } from "@/api/types";
import type { LibraryKindConfig } from "./libraryConfig";
import "./Browse.css";

// The facet params that live in the URL as repeated keys. Search, sort, and the
// watched toggle are single-valued and handled separately.
// Every filter parameter the bar owns. Kept as one list so "Clear all" cannot
// drift from what the bar can set -- a clear that misses a key leaves a grid
// filtered by something with no visible control.
const FACET_KEYS = FILTER_PARAM_KEYS;

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
  const [tileStep] = useTileSize();
  const [params, setParams] = useSearchParams();
  const q = params.get("q") ?? "";
  // The default comes from the kind rather than being "title" for everyone: a
  // picture library sorted by title is sorted by UUID, which is no order at
  // all. Each config lists its natural default first.
  const sort = params.get("sort") ?? config.sorts[0].value;
  const genres = params.getAll("genre");
  const decades = params.getAll("decade");
  const contentRatings = params.getAll("content_rating");
  const years = params.getAll("year");
  const resolutions = params.getAll("resolution");
  const people = params.getAll("person");
  const actors = params.getAll("actor");
  const directors = params.getAll("director");
  const collections = params.getAll("collection");
  /*
   * Face groups (ADR 0052), set from the People page rather than the bar.
   *
   * Plural because one person is often several groups — naming does not merge
   * them — so a tile hands over every id it collapsed and they are all this
   * person.
   */
  const faceClusters = params.getAll("face_cluster");
  const minRating = Number(params.get("min_rating") ?? 0);
  const status = params.get("status") ?? "";
  const unwatched = params.get("watched") === "false";
  // The A–Z rail's selection lives in the URL like every other control here, so
  // "the S films" is a link, survives a reload, and comes back with Back.
  const initial = params.get("initial") ?? "";

  /*
   * Putting a whole music library on.
   *
   * "Play all" is the library in title order; "Shuffle" is the same queue with
   * the player's own shuffle turned on rather than a second randomiser beside
   * it — so turning shuffle off afterwards gives the ordered library back,
   * which is the behaviour that already exists everywhere else.
   *
   * Shuffle opens on a random song because the shuffled order begins with
   * whatever it was handed and the rest follows at random — one randomiser, not
   * two.
   *
   * The queue is handed over in history state rather than in ?queue=: every
   * track of a library is far too much URL. See the Player.
   */
  const navigate = useNavigate();
  // One definition of what a poster offers, shared with search, collections
  // and a detail page's children. See useItemActions.
  const { actions: gridActions, dialogs: gridDialogs } = useItemActions();
  const qc = useQueryClient();
  /*
   * What "play all" queues here, or null where it means nothing.
   *
   * Music queues tracks, films queue films, a show library queues **episodes** —
   * a queue of containers is not something a player can advance through.
   * Pictures are excluded on purpose: a slideshow has a pace and a control of
   * its own, and dressing it up as a queue would ship the wrong thing quickly.
   */
  const playKind = playableKindFor(library.kind);
  const [gathering, setGathering] = useState<"play" | "shuffle" | null>(null);

  const playEverything = async (shuffle: boolean) => {
    if (gathering) return;
    setGathering(shuffle ? "shuffle" : "play");
    try {
      const ids = await fetchLibraryTracks(qc, libraryID, playKind ?? "track");
      const start = startOf(ids, shuffle);
      if (start === undefined) return;
      /*
       * The start is chosen here, not left to shuffle.
       *
       * This used to pass ids[0] always, on the reasoning that shuffle
       * randomises anyway. It does not: shuffledStartingWith *pins* the id it
       * is given to the front, so Randomize all began with the same film every
       * time and shuffled everything behind it. See startOf.
       */
      navigate(`/watch/${start}`, { state: { queue: ids, shuffle } });
    } finally {
      // Cleared even on failure, so a button cannot sit reading "Gathering…"
      // for the rest of the session.
      setGathering(null);
    }
  };

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
    years: years.map(Number),
    resolutions,
    people: people.map(Number),
    actors: actors.map(Number),
    directors: directors.map(Number),
    collections: collections.map(Number),
    faceClusters: faceClusters.map(Number),
    minRating,
    status,
    /*
     * Containers that group items are not in the grid.
     *
     * They group things rather than being them, and a franchise tile beside its
     * own members made a curated shelf read as an unsorted one. Each has its own
     * page.
     *
     * Playlists were missing from this list and it showed: every `.m3u` a scene
     * release ships was imported, and every one of them stood in the *artist*
     * grid as a tile with a release-folder name, beside the artists whose tracks
     * were on it. Not restricted to movie libraries any more either, which is
     * how a music library came to be the one place this rule did not apply.
     *
     * Except while searching, where hiding a matching collection or playlist
     * would be the search lying about what is here.
     */
    // Must stay the same set the server's library count excludes
    // (store.GroupingKinds), or the sidebar number and this grid describe
    // different things — which is exactly what "1,381 items" beside a grid of
    // 1,211 was.
    excludeKind: q ? undefined : "collection,playlist",
    initial: initial || undefined,
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
        for (const k of [...FACET_KEYS, "q"]) prev.delete(k);
        return prev;
      },
      { replace: true },
    );
  };

  // A sentinel below the grid pulls the next page into view as the user
  // reaches it, so a large library scrolls continuously instead of stopping at
  // the first page with no sign there is more. Shared with the Collections
  // page, which was built without it and truncated silently at 120.
  const sentinel = useRef<HTMLDivElement | null>(null);
  useInfiniteScroll(sentinel, {
    hasNextPage,
    isFetchingNextPage,
    fetchNextPage,
  });

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
  /*
   * Names for the people currently filtered on.
   *
   * The filter is by id because that is what identifies a person, but a pill
   * has to say a name -- and on a bookmarked URL the id arrives with nothing
   * attached, having never passed through the search panel that knew it.
   */
  const castLookup = useCastByIDs(libraryID, [
    ...people,
    ...actors,
    ...directors,
  ]);
  const castNames = useMemo(() => {
    const m = new Map<string, string>();
    for (const p of castLookup.data?.people ?? []) m.set(String(p.id), p.name);
    return m;
  }, [castLookup.data]);

  /*
   * And the name for a face-group pill.
   *
   * Fetched only while such a filter is active — the list is every person in
   * the library, and a movie library asking for it on every mount would be a
   * request that can never answer anything.
   *
   * A separate map from castNames on purpose. Both are keyed by small integers
   * from unrelated populations, so one map would resolve face group 3 to
   * whichever person happened to be credit 3 and print a confident wrong name.
   */
  const facePeople = useFacePeople(libraryID, faceClusters.length > 0);
  const faceNames = useMemo(() => {
    const m = new Map<string, string>();
    for (const p of facePeople.data?.people ?? []) {
      if (p.name) m.set(String(p.id), p.name);
    }
    return m;
  }, [facePeople.data]);

  const filtered =
    !!q ||
    genres.length > 0 ||
    decades.length > 0 ||
    contentRatings.length > 0 ||
    years.length > 0 ||
    resolutions.length > 0 ||
    people.length > 0 ||
    actors.length > 0 ||
    directors.length > 0 ||
    collections.length > 0 ||
    faceClusters.length > 0 ||
    minRating > 0 ||
    !!status ||
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
          {/* The library's size, not how much of it has arrived.
                "120 of 1,198" was the v0.3.2 fix for a grid that genuinely
                truncated at one page under a count claiming the full total.
                Paging removed the truncation; the label outlived it, and what
                it says at rest is that a 1,198-film library holds 120 -- a
                number that then creeps upward as you scroll, which reads as a
                counter that cannot make its mind up.

                Nothing is lost by dropping it: how much has loaded is already
                said where it matters, by the "Loading more" strip at the bottom
                edge of the grid, which is where somebody waiting for more items
                is actually looking. */}
          {data && (
            <span className="browse__count">{total.toLocaleString()}</span>
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
        <TileSizeSlider />
      </div>

      <div className="browse__controls">
      {facets?.initials && (
        <AlphabetRail
          initials={facets.initials}
          selected={initial}
          onPick={(c) => setParam("initial", c)}
        />
      )}

      <div className="browse__filters">
        {/* Every library whose contents are a queue. Pictures are the
            exception and stay one until a slideshow exists to put here. */}
        {playKind && (
          <div className="browse__playall">
            <button
              className="browse__playall-btn"
              onClick={() => void playEverything(false)}
              disabled={gathering !== null}
            >
              <span aria-hidden="true">▶</span>{" "}
              {gathering === "play" ? "Gathering…" : "Play all"}
            </button>
            <button
              className="browse__playall-btn"
              onClick={() => void playEverything(true)}
              disabled={gathering !== null}
            >
              <ShuffleGlyph size={15} />{" "}
              {gathering === "shuffle" ? "Gathering…" : shuffleLabel(playKind)}
            </button>
          </div>
        )}
        {/* Playlists, beside the play controls rather than in a tab strip: the
            header already carries search, a sort and the facet row, and a tab
            strip would compete with the facets for the same line and the same
            gesture. Only where the config says so — a control that leads to an
            empty grid in every library anyone has is spent space. */}
        {/* Collections have their own page for movie libraries. Only there:
            a shows library groups by series already, and music and pictures
            have no collections at all. */}
        {library.kind === "movie" && (
          <button
            className="browse__playall-btn"
            onClick={() => navigate(`/library/${libraryID}/collections`)}
          >
            Collections
          </button>
        )}
        {/* A picture library has a second way to be read: by when the
            photographs were taken rather than by which folder they are in. */}
        {isPictures && (
          <button
            className="browse__playall-btn"
            onClick={() => navigate(`/library/${libraryID}/timeline`)}
          >
            Timeline
          </button>
        )}
        {/* Face groups. Offered whether or not the worker is installed: the
            page itself explains an absence, which is better than a control
            that silently is not there. */}
        {isPictures && (
          <button
            className="browse__playall-btn"
            onClick={() => navigate(`/library/${libraryID}/people`)}
          >
            People
          </button>
        )}
        {config.playlists && (
          <button
            className="browse__playall-btn"
            onClick={() => navigate(`/library/${libraryID}/playlists`)}
          >
            Playlists
          </button>
        )}
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

        <FilterBar
          libraryID={libraryID}
          facets={facets}
          params={params}
          castNames={castNames}
          faceNames={faceNames}
          onToggle={toggleParam}
          onSet={setParam}
          onClear={clearFilters}
        />

        {filtered && (
          <button className="browse__clear" onClick={clearFilters}>
            Clear
          </button>
        )}
      </div>
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

      {/*
        The tile size is written as a custom property on the grid rather than on
        the document root. A root-level write is a global that outlives the
        screen that set it: every other grid in the app reads the same token,
        and a size chosen in a movie library would silently follow you into
        playlists and search. Scoped here, the slider changes the grid it sits
        above and nothing else.
      */}
      <div
        className="browse__grid"
        style={{ "--tile-grid": tileWidth(tileStep) } as CSSProperties}
      >
        {/*
          The library's own grid is one of the two places a cover may be lifted
          (ADR 0051, amended). Accepting here is how somebody reaches a folder
          they marked; everywhere outside the pictures the cover is fixed.
        */}
        <SensitiveReveal>
          {items.map((item) => (
            <PosterTile key={item.id} item={item} actions={gridActions} />
          ))}
        </SensitiveReveal>
      </div>

      {/* The sentinel needs real height: a zero-area element never reports as
          intersecting, so an unsized marker silently never triggers the next
          page. It doubles as the "loading more" strip. */}
      {hasNextPage && (
        <div ref={sentinel} className="browse__more">
          {isFetchingNextPage ? "Loading more…" : ""}
        </div>
      )}

      {/* The menu's dialogs. Rendered by the surface rather than by the menu,
          which unmounts the instant an item is picked -- see useItemActions. */}
      {gridDialogs}
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


/*
 * What the shuffle button is called.
 *
 * "Shuffle" is the word for music and has been since the mini-player shipped.
 * For films and episodes the same action is more naturally read as randomising
 * the library, and calling it Shuffle there invites the question of whether it
 * shuffles within a show. It does not: it randomises the whole queue.
 */
function shuffleLabel(kind: PlayableKind): string {
  return kind === "track" ? "Shuffle" : "Randomize all";
}
