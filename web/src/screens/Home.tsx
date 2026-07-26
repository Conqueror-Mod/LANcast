import {
  useLibraries,
  useContinueWatching,
  useRecentlyAdded,
  useItems,
} from "@/api/hooks";
import { Shelf } from "@/components/Shelf";
import type { Library } from "@/api/types";

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

// Home is the hub: continue watching → recently added → per-library shelves.
// Library names in the nav still jump straight to the full grid — the hubs are a
// convenience, never a gate.
export function Home() {
  const { data: libraries } = useLibraries();
  const { data: continueWatching } = useContinueWatching();
  const { data: recentlyAdded } = useRecentlyAdded();

  const hasAnything =
    (continueWatching?.length ?? 0) > 0 ||
    (recentlyAdded?.length ?? 0) > 0 ||
    (libraries?.length ?? 0) > 0;

  return (
    <div className="home">
      <Shelf title="Continue Watching" items={continueWatching ?? []} />
      <Shelf title="Recently Added" items={recentlyAdded ?? []} />
      {libraries?.map((lib) => (
        <LibraryShelf key={lib.id} library={lib} />
      ))}
      {!hasAnything && (
        <p style={{ color: "var(--text-muted)", padding: "40px 0" }}>
          No libraries yet. Add one from Settings.
        </p>
      )}
    </div>
  );
}
