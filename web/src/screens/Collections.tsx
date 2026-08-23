import { useRef } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { useInfiniteItems, useLibraries } from "@/api/hooks";
import { useInfiniteScroll } from "@/lib/useInfiniteScroll";
import { useBackHandler } from "@/focus/FocusController";
import { PosterTile } from "@/components/PosterTile";
import { useItemActions } from "@/components/itemActions";
import {
  AlphabetRail,
  initialsOf,
  matchesInitial,
} from "@/components/AlphabetRail";
import "./Browse.css";

/*
 * Collections, on their own.
 *
 * They used to sit in the library grid among the films they group: a franchise
 * tile beside its own members, in a grid whose question is "what have I got".
 * A collection answers a different question — "what belongs together" — and
 * mixing the two made the grid read as unsorted rather than as curated.
 *
 * A page rather than a filter chip, for the same reason playlists got one: it
 * is a place, and a place can be linked to, returned to, and found again.
 */
export function Collections() {
  // The same menu a library grid offers; a poster is a poster.
  const actions = useItemActions();
  const [params, setParams] = useSearchParams();
  const { id } = useParams();
  const libraryID = Number(id);
  const navigate = useNavigate();
  const { data: libraries } = useLibraries();
  const library = libraries?.find((l) => l.id === libraryID);

  const {
    data,
    isLoading,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useInfiniteItems({
    libraryID,
    kind: "collection",
    sort: "title",
  });
  const all = (data?.pages ?? []).flatMap((p) => p.items);
  /*
   * Filtered here rather than at the server, unlike the library grid.
   *
   * A library holds thousands of items and pages them in, so its rail has to
   * ask the server — the S titles may not be loaded. A collections page holds
   * everything it has, so the same rail can filter in memory, and asking the
   * server would be a round trip to sort a list already on screen.
   */
  const initial = params.get("initial") ?? "";
  const items = all.filter((i) => matchesInitial(i.title, initial));
  const total = data?.pages[0]?.total ?? 0;

  /*
   * The page pages, like every other listing.
   *
   * It did not, and the omission was invisible: one page of 120 rendered, the
   * count read "120", and every collection after roughly "H" was unreachable
   * with nothing on screen to say so. The rail made it worse rather than
   * better — it filters in memory over `all`, so a letter that had not loaded
   * simply had no collections in it.
   */
  const sentinel = useRef<HTMLDivElement | null>(null);
  useInfiniteScroll(sentinel, {
    hasNextPage,
    isFetchingNextPage,
    fetchNextPage,
  });

  const back = () => navigate(`/library/${libraryID}`);
  useBackHandler(back);

  return (
    <div className="browse">
      <div className="browse__head browse__head--sticky">
        <button className="pl-back" onClick={back}>
          ← {library?.name ?? "Library"}
        </button>
        <h1 className="browse__title">Collections</h1>
        {/* The server's total, not the number loaded — `items.length` read
            "120" on a library with 170 collections, which is the paging bug
            wearing a number. Filtering by letter narrows what is shown, so the
            filtered count leads when the rail is in use. */}
        <span className="browse__count">
          {initial
            ? items.length.toLocaleString()
            : total.toLocaleString() || ""}
        </span>
      </div>

      {!isLoading && items.length === 0 && (
        <p className="browse__message">
          No collections here yet. They arrive with metadata: a film that belongs
          to a franchise brings its collection with it, and a collection appears
          once at least two of its films are in this library.
        </p>
      )}

      <AlphabetRail
        initials={initialsOf(all.map((i) => i.title))}
        selected={initial}
        onPick={(c) => {
          const next = new URLSearchParams(params);
          if (c) next.set("initial", c);
          else next.delete("initial");
          setParams(next);
        }}
      />

      <div className="browse__grid">
        {items.map((item) => (
          <PosterTile key={item.id} item={item} actions={actions} />
        ))}
      </div>
      <div ref={sentinel} aria-hidden="true" />
    </div>
  );
}
