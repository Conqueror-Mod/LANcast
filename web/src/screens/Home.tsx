import {
  useLibraries,
  useContinueWatching,
  useRecentlyAdded,
  useItems,
} from "@/api/hooks";
import { Shelf } from "@/components/Shelf";
import { HomeHero } from "@/components/HomeHero";
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
function pickHero(
  resumable: Item[] | undefined,
  recent: Item[] | undefined,
): { item: Item; resuming: boolean } | null {
  const withArt = (items: Item[] | undefined) =>
    items?.find((i) => i.artwork?.fanart && !i.missing);

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
  const { data: recentlyAdded } = useRecentlyAdded();

  const hero = pickHero(continueWatching, recentlyAdded);

  // The hero already shows this item at full size. Repeating it as the first
  // tile of the shelf directly beneath is the kind of duplication that makes a
  // home page feel automatically generated rather than arranged.
  const withoutHero = (items: Item[] | undefined) =>
    hero ? (items ?? []).filter((i) => i.id !== hero.item.id) : (items ?? []);

  const hasAnything =
    (continueWatching?.length ?? 0) > 0 ||
    (recentlyAdded?.length ?? 0) > 0 ||
    (libraries?.length ?? 0) > 0;

  return (
    <div className="home">
      {hero && <HomeHero item={hero.item} resuming={hero.resuming} />}
      <div className="home__shelves">
        <Shelf
          title="Continue Watching"
          items={withoutHero(continueWatching)}
        />
        <Shelf title="Recently Added" items={withoutHero(recentlyAdded)} />
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
