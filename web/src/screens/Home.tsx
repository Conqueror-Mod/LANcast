import {
  useLibraries,
  useContinueWatching,
  useMemories,
  useRecentlyAddedVideo,
  useRecentlyAddedMusic,
  useRecentPhotos,
  useItems,
  useSetWatchedByID,
} from "@/api/hooks";
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { usePlayback } from "@/playback/PlaybackProvider";
import { Shelf } from "@/components/Shelf";
import type { MenuAction } from "@/components/Menu";
import { HomeHero } from "@/components/HomeHero";
import { HomeMasthead } from "@/components/HomeMasthead";
import { isMusic, watchedVerb } from "@/lib/kind";
import type { Item, Library } from "@/api/types";
import { showContinueTarget } from "@/lib/continueShow";
import { useHeroSpotlight } from "@/lib/useHero";
import "./Home.css";

/*
 * Films you have not started, shuffled.
 *
 * This replaced two kinds of shelf that only repeated what was already on the
 * page: each library's first twenty titles in alphabetical order, which the
 * library buttons in the masthead and the library pages already offer, and
 * "Recently Played in …", which on a one-person server was Continue Watching
 * again with the finished things left in. Reported as redundancies.
 *
 * What neither could do is suggest something. So: unwatched films, a different
 * handful each time the page is opened. The shuffle is seeded once per visit
 * (`sort=random&seed=`), so marking one watched refetches the row without
 * reshuffling it under the pointer.
 *
 * Started films are left out as well as finished ones — they are on Continue
 * Watching, and the server's `watched=false` counts a half-watched film as
 * unwatched. The server is asked for a few more than the row shows so that
 * dropping them does not leave it short.
 */
function UnwatchedShelf({
  library,
  seed,
  named,
  hide,
}: {
  library: Library;
  seed: number;
  /** Say which library, when there is more than one to tell apart. */
  named: boolean;
  /** The film the hero is already showing. */
  hide?: number;
}) {
  const { data } = useItems({
    libraryID: library.id,
    sort: "random",
    seed,
    unwatched: true,
    excludeKind: "collection,playlist",
    limit: 30,
  });
  const items = (data?.items ?? [])
    .filter((i) => !(i.progress && i.progress.position_ms > 0))
    .filter((i) => i.id !== hide)
    .slice(0, 20);
  return (
    <Shelf
      title={named ? `Unwatched in ${library.name}` : "Unwatched"}
      items={items}
      // The library grid, with its own Unwatched filter already on.
      seeAllTo={`/library/${library.id}?watched=false`}
    />
  );
}

// Home is the hub: a spotlight, then continue watching → recently added →
// something unwatched. Library names in the nav still jump straight to the full
// grid — the hubs are a convenience, never a gate.
export function Home() {
  const { data: libraries } = useLibraries();
  const { data: continueWatching } = useContinueWatching();
  // Deeper than the row can show, because the one list is split into two. On a
  // library where a scan just added 200 tracks, the top 20 by date are all
  // music and "Recently Added" would be empty while New Music overflowed.
  /*
   * A window each, rather than one window split afterwards.
   *
   * Sharing a window meant a bulk import of one kind evicted the others: a
   * music library arriving put 35 artists into the newest 40 rows and left the
   * films shelf with three entries, then with none. Asking per kind is what
   * keeps the most recent films visible however much music turns up.
   */
  const { data: recentlyAddedVideo } = useRecentlyAddedVideo(40);
  const { data: recentlyAddedMusic } = useRecentlyAddedMusic(20);
  // Photos come from their own query rather than out of recentlyAdded: that one
  // is top-level, so on a picture library it answers with galleries — the same
  // 25 folders every time, which is not what "recently added" means when you
  // are looking at photographs.
  const { data: recentPhotos } = useRecentPhotos(20);
  const { data: memories } = useMemories(20);

  // One shuffle per visit to the page; see UnwatchedShelf.
  const [seed] = useState(() => Math.floor(Math.random() * 2 ** 31));
  const filmLibraries = (libraries ?? []).filter((l) => l.kind === "movie");

  const setWatched = useSetWatchedByID();
  const navigate = useNavigate();
  const pb = usePlayback();

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
        label: `Mark as ${watchedVerb(item).past}`,
        onSelect: () => setWatched.mutate({ itemID: item.id, watched: true }),
      },
      {
        label: audio
          ? "Remove from Continue Listening"
          : "Remove from Continue Watching",
        onSelect: () => setWatched.mutate({ itemID: item.id, watched: false }),
      },
      {
        label: "Play next",
        onSelect: () => pb.playNextUp(item.id),
      },
      {
        label: "Add to queue",
        onSelect: () => pb.addToQueue(item.id),
      },
      {
        label: "Go to details",
        onSelect: () => navigate(`/item/${item.id}`),
      },
    ];
  };

  /*
   * Which item is in the spotlight, and why.
   *
   * The rule used to be fixed — first resumable item with fanart, else first
   * recently added — and resume-wins is right for the common case and wrong for
   * the one people complain about: a library nobody is mid-way through, where
   * the hero becomes a permanent monument to the last thing anybody watched.
   * The choice is a device setting now, and the modes that cost extra requests
   * make them only when they are the chosen one.
   */
  const hero = useHeroSpotlight(recentlyAddedVideo);

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

  // Already the right kinds, so only the hero has to be taken out of the video
  // row; the music row cannot contain it.
  const recentVideo = withoutHero(recentlyAddedVideo);
  const recentAudio = recentlyAddedMusic ?? [];
  const recentPictures = (recentPhotos ?? []).filter((i) => !i.missing);
  /*
   * On this day. Empty on most days, and the Shelf renders nothing when it is —
   * a heading over no tiles is the shape of something broken.
   *
   * Not filtered for `missing` the way recentPictures is: the server already
   * excludes them, along with marked folders and this year's photographs, and a
   * second opinion here would be a rule in two places that can disagree.
   */
  const onThisDay = memories?.items ?? [];

  /*
   * Pressing a show on this shelf continues the show; pressing a film opens
   * the film, which is what the default already does.
   *
   * The tile carries `next_episode` so it can draw the right title and bar,
   * and deliberately does not use it to decide what to play — showContinueTarget
   * re-asks the server, because this list is up to ten seconds old and landing
   * on an episode already finished is the exact failure that endpoint refuses
   * to cache against.
   *
   * A failure here navigates to the show instead of dying silently: the shelf
   * is not a page that can grow an error banner, and the show page can both
   * explain itself and offer Play.
   */
  const continueOpen = (item: Item) => {
    if (item.kind !== "show") return undefined;
    return () => {
      void (async () => {
        try {
          const target = await showContinueTarget(item.id);
          if (target.kind !== "play") {
            navigate(`/item/${item.id}`);
            return;
          }
          navigate(`/watch/${target.episodeID}`, {
            state: { queue: target.queue },
          });
        } catch {
          navigate(`/item/${item.id}`);
        }
      })();
    };
  };

  const hasAnything =
    (continueWatching?.length ?? 0) > 0 ||
    (recentlyAddedVideo?.length ?? 0) > 0 ||
    (recentlyAddedMusic?.length ?? 0) > 0 ||
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
      {hero && <HomeHero item={hero.item} reason={hero.reason} seed={hero.seed} />}
      <div className="home__shelves">
        <Shelf
          title="Continue Watching"
          items={continueVideo}
          itemActions={continueActions}
          itemOpen={continueOpen}
        />
        <Shelf
          title="Continue Listening"
          items={continueAudio}
          itemActions={continueActions}
        />
        <Shelf title="Recently Added" items={recentVideo} />
        <Shelf title="New Music" items={recentAudio} />
        <Shelf title="Recently Added Photos" items={recentPictures} />
        {/* After the recent shelves rather than before: "what is new" is the
            question somebody opened the page with, and "what happened on this
            date years ago" is the one worth finding once they are here. */}
        <Shelf title="On this day" items={onThisDay} />
        {filmLibraries.map((lib) => (
          <UnwatchedShelf
            key={lib.id}
            library={lib}
            seed={seed}
            named={filmLibraries.length > 1}
            hide={hero?.item.id}
          />
        ))}
      </div>
      {!hasAnything && (
        <p className="home__empty">No libraries yet. Add one from Settings.</p>
      )}
    </div>
  );
}
