import { useCallback } from "react";
import { useNavigate } from "react-router-dom";
import { useSetWatchedByID } from "@/api/hooks";
import { usePlayback } from "@/playback/PlaybackProvider";
import { isContainer, isPicture, watchedVerb } from "@/lib/kind";
import type { MenuAction } from "./Menu";
import type { Item } from "@/api/types";

/*
 * What a right-click offers on a poster, wherever the poster is.
 *
 * Written once because there are four callers — the library grid, search
 * results, a collection, and the children of a detail page — and they are the
 * same question every time: this is a thing, what can I do to it. Four inline
 * copies would answer it four ways within a release, and the one that drifted
 * would be whichever surface nobody opened that week.
 *
 * It stays a *function of the item* rather than a fixed list, because most of a
 * library has no answer. A grid holds shows, seasons, albums and galleries
 * beside films; "Play" on a folder is not a smaller version of playing, it is a
 * different action this menu would be guessing at, and a photograph is neither
 * watched nor possessed of a detail page worth visiting. Those get nothing,
 * and PosterTile turns nothing into no menu at all rather than an empty one.
 *
 * The Continue shelves deliberately do *not* use this. They offer removal from
 * the shelf, which is meaningless anywhere else, and they are the one place
 * where "remove" has two honest meanings that have to be spelled out. A shared
 * list with a flag for that would be one function pretending to be two.
 */
export function useItemActions(): (item: Item) => MenuAction[] {
  const navigate = useNavigate();
  const setWatched = useSetWatchedByID();
  const pb = usePlayback();

  return useCallback(
    (item: Item): MenuAction[] => {
      if (isPicture(item) || isContainer(item)) return [];
      const verb = watchedVerb(item);
      const seen = item.progress?.watched ?? false;
      return [
        { label: "Play", onSelect: () => navigate(`/watch/${item.id}`) },
        {
          label: seen ? `Mark as ${verb.negated}` : `Mark as ${verb.past}`,
          onSelect: () => setWatched.mutate({ itemID: item.id, watched: !seen }),
        },
        { label: "Play next", onSelect: () => pb.playNextUp(item.id) },
        { label: "Add to queue", onSelect: () => pb.addToQueue(item.id) },
        { label: "Go to details", onSelect: () => navigate(`/item/${item.id}`) },
      ];
    },
    [navigate, setWatched, pb],
  );
}
