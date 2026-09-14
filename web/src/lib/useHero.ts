import { useItem, useItems, useContinueWatching } from "@/api/hooks";
import type { Item } from "@/api/types";
import {
  pickHero,
  seedGenres,
  useHeroMode,
  usePinnedHero,
  type HeroPick,
} from "./heroMode";

/*
 * Gathering what the spotlight needs, without asking for what it does not.
 *
 * Every extra query here is one more request on the first screen of the app, so
 * each mode pays only for itself: the seed lookup and the candidate search are
 * disabled outside "recommended", and the pinned item is not fetched unless
 * something is pinned. Home already asks for Continue Watching and Recently
 * Added for its own shelves, so the two default modes cost nothing at all.
 *
 * A note on what "recommended" is, because the honest description is the whole
 * design. LANcast has no recommender and must not grow one that phones home.
 * This is not an engine: it takes the thing you are part-way through, asks what
 * genres it carries, and finds something unwatched in the same library that
 * shares one. It can always say where it came from — the hero renders that
 * sentence — and a suggestion that cannot be attributed is not shown at all.
 */
export function useHeroSpotlight(recent: Item[] | undefined): HeroPick | null {
  const [mode] = useHeroMode();
  const [pinnedID] = usePinnedHero();
  const { data: continueWatching } = useContinueWatching();

  /*
   * The seed is whatever you are already part-way through. It is the one thing
   * on this page that is a fact about the person looking at it — everything
   * else is a fact about the library.
   *
   * Fetched in full because the list shape carries no genres: they are a detail
   * response only, which is one request, once, and only in this mode.
   */
  const wanted = mode === "recommended";
  const seedID = wanted ? (continueWatching?.[0]?.id ?? 0) : 0;
  const { data: seed } = useItem(seedID);

  const genres = seedGenres(seed);
  const { data: candidates } = useItems({
    // A zero library id is how every query in this file switches itself off.
    libraryID: wanted && genres.length > 0 ? (seed?.library_id ?? 0) : 0,
    genres,
    unwatched: true,
    // Newest first: among things you have not seen, the one that arrived most
    // recently is the one you are least likely to have already decided against.
    sort: "added",
    limit: 20,
  });

  const { data: pinned } = useItem(mode === "pinned" ? pinnedID : 0);

  /*
   * Neither the seed nor anything already on the Continue shelf may be
   * suggested. A part-watched film is unwatched as far as the filter is
   * concerned, so without this the recommendation is liable to be the very
   * thing it was derived from — which reads as a bug, and is one.
   */
  const onTheShelf = new Set((continueWatching ?? []).map((i) => i.id));
  const recommended = (candidates?.items ?? []).filter(
    (i) => i.id !== seedID && !onTheShelf.has(i.id),
  );

  return pickHero(mode, {
    resumable: continueWatching,
    recent,
    recommended,
    seed: recommended.length > 0 ? seed : undefined,
    pinned,
  });
}
