import { useTrending } from "@/api/hooks";
import { Shelf } from "./Shelf";
import type { Library } from "@/api/types";

/*
 * What a library's people have been playing lately.
 *
 * The whole design problem here is the title, not the list. `viewers` counts
 * accounts rather than plays, so on a single-account server every entry is 1
 * and the row is honestly *this* person's recent activity — calling that
 * "Trending" would be a small lie told on the home page every day.
 *
 * So the server reports how many accounts contributed, and the row names
 * itself accordingly: one contributor gets "Recently Played in Films", several
 * get "Trending in Films". Same data, and the word matches what the data can
 * support.
 *
 * It renders nothing rather than an empty row when there is no activity. A
 * shelf with a heading and no tiles is the shape of something broken.
 */
export function TrendingShelf({ library }: { library: Library }) {
  const { data } = useTrending(library.id);

  const items = (data?.items ?? []).map((t) => t.item);
  if (items.length === 0) return null;

  // Two is where "trending" starts meaning something. One account is a diary.
  const shared = (data?.contributors ?? 0) > 1;

  return (
    <Shelf
      title={`${shared ? "Trending in" : "Recently Played in"} ${library.name}`}
      items={items}
    />
  );
}
