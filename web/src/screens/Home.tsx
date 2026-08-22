import {
  useLibraries,
  useContinueWatching,
  useRecentlyAdded,
  useRecentPhotos,
  useItems,
  useSetWatchedByID,
} from "@/api/hooks";
import { useNavigate } from "react-router-dom";
import { Shelf } from "@/components/Shelf";
import type { MenuAction } from "@/components/Menu";
import { HomeHero } from "@/components/HomeHero";
import { HomeMasthead } from "@/components/HomeMasthead";
import { TrendingShelf } from "@/components/TrendingShelf";
import { isMusic, isPicture } from "@/lib/kind";
import type { Item, Library } from "@/api/types";
import "./Home.css";

// One library's own shelf. A component per library so each owns its query
// without calling hooks in a loop.
function LibraryShelf({ library }: { library: Library }) {
  const { data } = useItems({ libraryID: library.id, limit: 20 });
  return (
    <Shelf
      title={library.name}
      items={data?.items ?? []}
      seeAllTo={`/library/${library.id}`}
    />
  );
}

// The hero needs a backdrop to be a hero at all, so the pick is the first
// candidate that actually has fanart rather than simply the first candidate.
// Resume wins over new: it is the likeliest reason someone opened LANcast.
//
// Music and pictures are excluded rather than left to fail the fanart test.
// Neither has a backdrop today and both would be skipped anyway, but "the hero
// is for something you watch" is the actual rule, and leaving it implicit means
// the first album that arrives with provider artwork — or the first photo wide
// enough to look like one — silently becomes a hero.
function pickHero(
  resumable: Item[] | undefined,
  recent: Item[] | undefined,
): { item: Item; resuming: boolean } | null {
  const withArt = (items: Item[] | undefined) =>
    items?.find(
      (i) => i.artwork?.fanart && !i.missing && !isMusic(i) && !isPicture(i),
    );

  const inProgress = withArt(resumable);
  if (inProgress) return { item: inProgress, resuming: true };

  const fresh = withArt(recent);
  if (fresh) return { item: fresh, resuming: false };

  return null;
}

// Home is the hub: a spotlight, then continue watching → recently added →
// per-library shelves. Library names in the nav still jump straight to the full
// grid — the hubs are a convenience, never a gate.
export function Home() {
  const { data: libraries } = useLibraries();
  const { data: continueWatching } = useContinueWatching();
  // Deeper than the row can show, because the one list is split into two. On a
  // library where a scan just added 200 tracks, the top 20 by date are all
  // music and "Recently Added" would be empty while New Music overflowed.
  const { data: recentlyAdded } = useRecentlyAdded(40);
  // Photos come from their own query rather than out of recentlyAdded: that one
  // is top-level, so on a picture library it answers with galleries — the same
  // 25 folders every time, which is not what "recently added" means when you
  // are looking at photographs.
  const { data: recentPhotos } = useRecentPhotos(20);

  const setWatched = useSetWatchedByID();
  const navigate = useNavigate();

  /*
   * What a right-click offers on a Continue shelf.
   *
   * Two ways off the shelf, and they are not the same thing said twice. An item
   * is on it because a position was saved and the watched flag is not set, so
   * it leaves either by admitting it was finished or by forgetting the
   * position — and those disagree about whether you have seen it.
   *
   * A single "Remove" would have to pick one silently. For a film abandoned
   * twenty minutes in, marking it watched is a lie the library then repeats
   * every time it filters by unwatched; for one finished on another device,
   * forgetting the position throws away the fact you watched it. Both are
   * offered because both are things people mean.
   *
   * The wording carries the difference rather than a tooltip: one says what it
   * records, the other names the shelf and claims nothing about whether it was
   * seen.
   *
   * And it says *watched* or *played* to match what the thing is. The same two
   * writes serve an album track and a film, but "Mark as watched" on a song and
   * "Remove from Continue Watching" on the Continue Listening shelf are both
   * the interface reading from the wrong half of itself — small, and exactly
   * the kind of small that makes a feature feel bolted on.
   */
  const continueActions = (item: Item): MenuAction[] => {
    const audio = isMusic(item);
    return [
      {
        label: audio ? "Mark as played" : "Mark as watched",
        onSelect: () => setWatched.mutate({ itemID: item.id, watched: true }),
      },
      {
        label: audio
          ? "Remove from Continue Listening"
          : "Remove from Continue Watching",
        onSelect: () => setWatched.mutate({ itemID: item.id, watched: false }),
      },
      {
        label: "Go to details",
        onSelect: () => navigate(`/item/${item.id}`),
      },
    ];
  };

  const hero = pickHero(continueWatching, recentlyAdded);

  // The hero already shows this item at full size. Repeating it as the first
  // tile of the shelf directly beneath is the kind of duplication that makes a
  // home page feel automatically generated rather than arranged.
  const withoutHero = (items: Item[] | undefined) =>
    hero ? (items ?? []).filter((i) => i.id !== hero.item.id) : (items ?? []);

  // Watching and listening are separate hubs. One mixed row put a half-played
  // track between two films, and because a track carries no cover the row read
  // as broken films rather than as music — the fault looked like missing
  // artwork when it was actually a missing distinction.
  //
  // Recently Added splits for a second reason on top of that one: a sleeve is
  // square and a poster is 2:3, so a mixed row is also a ragged row — the
  // tiles stop sharing a baseline and the shelf reads as broken alignment
  // rather than as two kinds of thing.
  const resumable = withoutHero(continueWatching);
  const continueVideo = resumable.filter((i) => !isMusic(i));
  const continueAudio = resumable.filter(isMusic);

  const recent = withoutHero(recentlyAdded);
  const recentVideo = recent.filter((i) => !isMusic(i) && !isPicture(i));
  const recentAudio = recent.filter(isMusic);
  const recentPictures = (recentPhotos ?? []).filter((i) => !i.missing);

  const hasAnything =
    (continueWatching?.length ?? 0) > 0 ||
    (recentlyAdded?.length ?? 0) > 0 ||
    (libraries?.length ?? 0) > 0;

  return (
    <div className="home">
      {/*
        The masthead runs above the hero when there is one and stands in for it
        when there is not — a home page whose first screenful is an empty grid
        is the state a new install spends its first hour in, and it is the one
        nobody designs for.
      */}
      <HomeMasthead libraries={libraries} hasHero={!!hero} />
      {hero && <HomeHero item={hero.item} resuming={hero.resuming} />}
      <div className="home__shelves">
        <Shelf
          title="Continue Watching"
          items={continueVideo}
          itemActions={continueActions}
        />
        <Shelf
          title="Continue Listening"
          items={continueAudio}
          itemActions={continueActions}
        />
        <Shelf title="Recently Added" items={recentVideo} />
        <Shelf title="New Music" items={recentAudio} />
        <Shelf title="Recently Added Photos" items={recentPictures} />
        {/* Activity before catalogue: what people have been playing is a
            livelier answer to "what now" than the same alphabetical grid the
            library page already gives. */}
        {libraries?.map((lib) => (
          <TrendingShelf key={`trend-${lib.id}`} library={lib} />
        ))}
        {libraries?.map((lib) => (
          <LibraryShelf key={lib.id} library={lib} />
        ))}
      </div>
      {!hasAnything && (
        <p className="home__empty">No libraries yet. Add one from Settings.</p>
      )}
    </div>
  );
}
