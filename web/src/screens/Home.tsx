import { QuickRate } from "@/components/QuickRate";
import {
  useLibraries,
  useContinueWatching,
  HOME_WATCHING_LIMIT,
  useMemories,
  useRecentlyAddedVideo,
  useRecentlyAddedMusic,
  useRecentlyAddedKind,
  useRecentPhotos,
  useItems,
  useWatchlist,
  useResume,
  useIsAdmin,
  usePeerPresence,
  useToRate,
  useItemCollections,
  watchlistItems,
} from "@/api/hooks";
import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { Shelf } from "@/components/Shelf";
import type { MenuAction } from "@/components/Menu";
import { HomeHero } from "@/components/HomeHero";
import { ResumeConsole } from "@/components/ResumeConsole";
import { NewThisWeek } from "@/components/NewThisWeek";
import { PinnedShelves } from "@/components/PinnedShelves";
import { FriendsPanel, MemoryPanel, showFriends } from "@/components/BridgePanels";
import { collectionsMenu } from "@/lib/collectionsMenu";
import { HomeMasthead } from "@/components/HomeMasthead";
import type { Item, Library } from "@/api/types";
import { useContinueMenu } from "@/lib/useContinueMenu";
import { useHeroSpotlight } from "@/lib/useHero";
import "./Home.css";
import "./Bridge.css";

/*
 * Films you have not started, shuffled — and shows you have not started.
 *
 * This replaced two kinds of shelf that only repeated what was already on the
 * page: each library's first twenty titles in alphabetical order, which the
 * library buttons in the masthead and the library pages already offer, and
 * "Recently Played in …", which on a one-person server was Continue Watching
 * again with the finished things left in. Reported as redundancies.
 *
 * What neither could do is suggest something. So: a different handful of
 * things you have not begun, each time the page is opened. The shuffle is
 * seeded once per visit (`sort=random&seed=`), so marking one watched
 * refetches the row without reshuffling it under the pointer.
 *
 * `status=unstarted` rather than `watched=false`, because a show has no play
 * state of its own — every show passes watched=false — and "unstarted" asks
 * about its episodes. It also leaves out a half-watched film, which is on
 * Continue Watching already.
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
  /** The item the hero is already showing. */
  hide?: number;
}) {
  const { data } = useItems({
    libraryID: library.id,
    sort: "random",
    seed,
    status: "unstarted",
    excludeKind: "collection,playlist",
    limit: 21,
  });
  const items = (data?.items ?? []).filter((i) => i.id !== hide).slice(0, 20);
  return (
    <Shelf
      title={named ? `Unwatched in ${library.name}` : "Unwatched"}
      items={items}
      // A film library's grid, with its own Unwatched filter already on. A
      // show library's grid has no filter that means "not begun", so it opens
      // as it is.
      seeAllTo={
        library.kind === "movie"
          ? `/library/${library.id}?watched=false`
          : `/library/${library.id}`
      }
    />
  );
}

// Home is the hub: the Bridge (spotlight and panels), then what is new this
// week → what to rate → what you meant to watch → your pinned views → something
// unwatched (ADR 0119 §2a). Library names in the nav still jump straight to the full
// grid — the hubs are a convenience, never a gate.
export function Home() {
  const { data: libraries } = useLibraries();
  /*
   * Continue Watching feeds the spotlight's resume mode and tells the
   * spotlight whether its title is in progress; the spotlight asks for the
   * same key, so it is one request. What is in progress is drawn by the
   * resume console below (ADR 0118), which asks the server for every kind.
   */
  const { data: continueWatching } = useContinueWatching("watching", HOME_WATCHING_LIMIT);
  const { data: resume } = useResume();
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
  const { data: recentBooks } = useRecentlyAddedKind("book");
  const { data: recentComics } = useRecentlyAddedKind("comic");
  const { data: recentGames } = useRecentlyAddedKind("rom");
  // Photos come from their own query rather than out of recentlyAdded: that one
  // is top-level, so on a picture library it answers with galleries — the same
  // 25 folders every time, which is not what "recently added" means when you
  // are looking at photographs.
  const { data: recentPhotos } = useRecentPhotos(20);
  const { data: memories } = useMemories(20);

  // One shuffle per visit to the page; see UnwatchedShelf.
  const [seed] = useState(() => Math.floor(Math.random() * 2 ** 31));
  // Film and show libraries: music and pictures have nothing to leave unwatched.
  const unwatchedLibraries = (libraries ?? []).filter(
    (l) => l.kind === "movie" || l.kind === "show",
  );

  const navigate = useNavigate();
  const admin = useIsAdmin();
  const { continueActions, resumeActions, resumeOpen } = useContinueMenu();

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
  // The collections the spotlight's title is in, for its More Options.
  const { data: heroCollections } = useItemCollections(
    hero && hero.item.kind !== "collection" ? hero.item.id : 0,
  );

  // The hero already shows this item at full size. Repeating it as the first
  // tile of the shelf directly beneath is the kind of duplication that makes a
  // home page feel automatically generated rather than arranged.
  const withoutHero = (items: Item[] | undefined) =>
    hero ? (items ?? []).filter((i) => i.id !== hero.item.id) : (items ?? []);

  /*
   * The console in place of Continue Watching, Next up and Continue
   * Listening (ADR 0118 §2). The spotlight's title is left out of it, for the
   * reason it was left out of the shelf beneath: the same thing twice, side by
   * side, is a page that looks generated rather than arranged.
   */
  const resumeCards = (resume?.cards ?? []).filter((c) => !hero || c.item.id !== hero.item.id);

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
  // What you finished on this date in an earlier year (ADR 0119 §3a).
  const watchedThisDay = memories?.watched ?? [];
  // Friends on paired servers (ADR 0118 §4). Shown with an empty roster only
  // to an administrator, who can pair.
  const { data: peerPresence } = usePeerPresence();
  const friendsShown = showFriends(peerPresence?.peers, admin);

  const { data: watchlist } = useWatchlist();
  const { data: toRate } = useToRate();

  const hasAnything =
    (continueWatching?.length ?? 0) > 0 ||
    (resume?.cards.length ?? 0) > 0 ||
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
      {/*
        The Bridge (ADR 0118 §1): the spotlight and the panels share the first
        screen. On a wide screen the panels stand over the right of the
        spotlight's backdrop, which stays full-bleed; narrower, they drop into
        a row beneath it; on a phone the console leads. Which goes where is
        CSS (Bridge.css); the order here is the reading order.
      */}
      <div className={"bridge" + (hero ? " bridge--hero" : "")}>
      {hero && (
        <HomeHero
          item={hero.item}
          reason={hero.reason}
          seed={hero.seed}
          taste={hero.taste}
          // The shelf's menu, when the spotlight is showing something from
          // the shelf: the one place a collection's next film or a resumed
          // title can be taken off without opening it.
          //
          // And "View collection" for a title in one, on the shelf or not:
          // the spotlight is often the first place a film is seen.
          actions={heroActions(
            (continueWatching ?? []).some((i) => i.id === hero.item.id)
              ? continueActions(hero.item)
              : [],
            collectionsMenu(hero.item, heroCollections?.items, (id) => navigate(`/item/${id}`)),
          )}
        />
      )}
        <div className="bridge__panels">
          <ResumeConsole
            cards={resumeCards}
            more={resume?.more ?? false}
            actions={resumeActions}
            open={resumeOpen}
            libraries={libraries}
          />
          {/* Side by side beneath the console; absent when neither has
              anything to say, so the column never ends in an empty frame. */}
          {(friendsShown || onThisDay.length > 0 || watchedThisDay.length > 0) && (
            <div className="bridge__pair">
              {friendsShown && <FriendsPanel peers={peerPresence?.peers ?? []} />}
              <MemoryPanel items={onThisDay} watched={watchedThisDay} />
            </div>
          )}
        </div>
      </div>
      <div className="home__shelves">
        {/* What arrived in the last seven days, and nothing older (ADR 0118):
            first under the spotlight. A quiet week draws nothing here, which
            is what lets Rate what you've watched lead instead. */}
        <NewThisWeek
          rows={{
            video: recentVideo,
            music: recentAudio,
            books: withoutHero(recentBooks),
            comics: recentComics,
            games: recentGames,
            photos: recentPictures,
          }}
        />
        {/* What you finished lately and have not rated. Rating one takes it
            off; an empty list is no shelf. */}
        <Shelf
          title="Rate what you've watched"
          items={toRate?.items ?? []}
          itemFooter={(item) => <QuickRate item={item} />}
        />
        {/* What you meant to watch (ADR 0085). Newest first, and empty means
            no shelf. */}
        <Shelf title="Watchlist" items={watchlistItems(watchlist)} />
        {/* The views you pinned from a library grid, in your order (ADR 0119 §2). */}
        <PinnedShelves libraries={libraries} />
        {unwatchedLibraries.map((lib) => (
          <UnwatchedShelf
            key={lib.id}
            library={lib}
            seed={seed}
            named={unwatchedLibraries.length > 1}
            hide={hero?.item.id}
          />
        ))}
      </div>
      {/*
        A new install (ADR 0118 §3): one welcome panel, and no empty frames
        around it. Adding a library is an administrator's to do, so anybody
        else is told what they are waiting for rather than shown a button that
        would fail.
      */}
      {!hasAnything && (libraries?.length ?? 0) === 0 && libraries !== undefined && (
        <section className="panel home__welcome" aria-labelledby="welcome-title">
          <h2 id="welcome-title" className="home__welcome-title">
            {admin ? "Add your first library" : "Nothing here yet"}
          </h2>
          <p className="home__welcome-body">
            {admin
              ? "Point LANcast at a folder of films, shows, music, books, comics, games or photographs. It scans the folder and everything here fills in from it."
              : "Libraries appear here once the server's administrator adds one."}
          </p>
          {admin && (
            <Link className="hero__btn hero__btn--primary home__welcome-go" to="/settings?pane=libraries">
              Add a library
            </Link>
          )}
        </section>
      )}
    </div>
  );
}

/*
 * The spotlight's More Options: the shelf's entries when its title is on the
 * shelf, with the collection entries before "Go to details", which stays last
 * as it is everywhere. No entries at all means no menu.
 */
export function heroActions(shelf: MenuAction[], collections: MenuAction[]): MenuAction[] | undefined {
  if (shelf.length === 0 && collections.length === 0) return undefined;
  const last = shelf.length > 0 && shelf[shelf.length - 1].label === "Go to details" ? shelf.length - 1 : shelf.length;
  return [...shelf.slice(0, last), ...collections, ...shelf.slice(last)];
}
