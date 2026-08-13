import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { useInfiniteItems, useLibraries } from "@/api/hooks";
import { useBackHandler } from "@/focus/FocusController";
import { PosterTile } from "@/components/PosterTile";
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
  const [params, setParams] = useSearchParams();
  const { id } = useParams();
  const libraryID = Number(id);
  const navigate = useNavigate();
  const { data: libraries } = useLibraries();
  const library = libraries?.find((l) => l.id === libraryID);

  const { data, isLoading } = useInfiniteItems({
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

  const back = () => navigate(`/library/${libraryID}`);
  useBackHandler(back);

  return (
    <div className="browse">
      <div className="browse__head browse__head--sticky">
        <button className="pl-back" onClick={back}>
          ← {library?.name ?? "Library"}
        </button>
        <h1 className="browse__title">Collections</h1>
        <span className="browse__count">{items.length || ""}</span>
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
          <PosterTile key={item.id} item={item} />
        ))}
      </div>
    </div>
  );
}
