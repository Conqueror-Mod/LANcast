import { useCallback, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import {
  fetchDescendantIDs,
  fetchShowEpisodes,
  useIsAdmin,
  useSetSensitive,
  useSetWatchedByID,
  useSettings,
} from "@/api/hooks";
import { usePlayback } from "@/playback/PlaybackProvider";
import { isContainer, isMusic, isPicture, watchedVerb } from "@/lib/kind";
import { startOf } from "@/playback/queueOrder";
import { AddToPlaylist } from "./AddToPlaylist";
import { RemoveDialog } from "./RemoveDialog";
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
 * It stays a *function of the item* rather than a fixed list, because a grid
 * holds shows, seasons, albums and galleries beside films and the honest answer
 * differs for each. What changed is that "differs" used to mean "is empty": a
 * container returned nothing, so PosterTile drew no menu, and right-clicking a
 * show — the most common tile in a television library — gave the browser's own.
 * A container has plenty to offer. It just cannot be offered the *leaf's* list,
 * because "Play" on a show is not a smaller version of playing a film.
 *
 * So there are three shapes below, and one thing they share: none of them
 * guesses. A photograph is still neither watched nor queued, and it says so by
 * offering neither rather than by offering both greyed out.
 *
 * The Continue shelves deliberately do *not* use this. They offer removal from
 * the shelf, which is meaningless anywhere else, and they are the one place
 * where "remove" has two honest meanings that have to be spelled out. A shared
 * list with a flag for that would be one function pretending to be two.
 */

/**
 * The hook returns a node as well as a function.
 *
 * Two of the new actions open a dialog, and a menu item cannot render one: the
 * menu unmounts the moment you pick something, taking any dialog it owned with
 * it. The next reach is for a portal onto document.body, which is a bigger
 * answer than the problem. So the *hook* holds which item a dialog is open for,
 * and hands the surface one node to drop at the end of its tree.
 *
 * That is also why this file is `.tsx`. It was `.ts` while a menu was only ever
 * a list of labels and closures.
 */
export type ItemActions = {
  actions: (item: Item) => MenuAction[];
  /** Render once, anywhere in the surface. Nothing while no dialog is open. */
  dialogs: React.ReactNode;
};

/*
 * The ids a container queues, from whichever route knows the answer.
 *
 * A show has a dedicated endpoint and must use it: episodes hang off seasons,
 * `/api/items/{id}/episodes` exists so that no client reimplements that walk,
 * and it also handles the shape the walk gets wrong -- a show whose episodes
 * sit directly under the show row. It orders by season and episode outright
 * rather than depending on every episode tying on sort_title, and it drops
 * missing episodes server-side.
 *
 * Everything else is a plain parent_id hierarchy with no endpoint of its own.
 */
async function queueFor(qc: QueryClient, item: Item): Promise<number[]> {
  if (item.kind === "show") {
    return (await fetchShowEpisodes(item.id)).map((e) => e.id);
  }
  return fetchDescendantIDs(qc, item.id);
}

export function useItemActions(): ItemActions {
  const navigate = useNavigate();
  const setWatched = useSetWatchedByID();
  const pb = usePlayback();
  const qc = useQueryClient();
  const isAdmin = useIsAdmin();
  const [playlistFor, setPlaylistFor] = useState<Item | null>(null);
  const [removeFor, setRemoveFor] = useState<Item | null>(null);
  /*
   * A container is gathered on the press, not on the render.
   *
   * A show is a request per season before its queue exists, and on anything
   * slower than a warm cache that is a menu item which appears to do nothing.
   * The flag is what lets the label say "Gathering…" instead — the same trade
   * LibraryView's Play all already makes, named the same way so the two read as
   * one idea rather than two.
   */
  const [gathering, setGathering] = useState(false);

  /*
   * Start a queue built from a container's leaves.
   *
   * Navigating to the first id with the rest in route state is how every other
   * "play all" in this client starts a queue (LibraryView, the detail page), so
   * it is how this one does. A second mechanism for the same thing would be a
   * second set of shuffle bugs.
   */
  const playContainer = useCallback(
    async (item: Item, shuffle: boolean) => {
      if (gathering) return;
      setGathering(true);
      try {
        const ids = await queueFor(qc, item);
        // Not ids[0]: shuffledStartingWith pins whatever it is handed to the
        // front, so a fixed start makes Shuffle mean "shuffle everything after
        // the first episode". See startOf.
        const start = startOf(ids, shuffle);
        if (start === undefined) return;
        navigate(`/watch/${start}`, { state: { queue: ids, shuffle } });
      } finally {
        // Cleared even on failure, so one request that timed out cannot leave
        // every menu on the screen permanently disabled.
        setGathering(false);
      }
    },
    [gathering, navigate, qc],
  );

  /*
   * Mark every leaf under a container.
   *
   * There is no bulk progress endpoint — `PUT /api/items/{id}/progress` is
   * per-item — and adding one is an API contract change, which is not something
   * a menu item gets to drag in behind it. So this is N writes over a LAN. A
   * season is twenty; the longest show in a real library is a few hundred, and
   * it is a thing somebody does once, deliberately.
   *
   * Batched rather than fired at once, because Promise.all over three hundred
   * fetches is three hundred sockets and the browser's own queueing turns that
   * into a page that stops answering anything else. `mutateAsync` rather than
   * `mutate` for the same reason: the point of a batch is to wait for it, and
   * `mutate` returns immediately, which would make the batching decorative.
   *
   * A failed write is swallowed rather than abandoning the run. Stopping at the
   * first error leaves the show half-marked *and* says nothing about where it
   * stopped, which is the worst of both.
   */
  const markAll = useCallback(
    async (item: Item, watched: boolean) => {
      if (gathering) return;
      setGathering(true);
      try {
        // The same route Play all uses, so "mark all as watched" and "play
        // all" cannot come to disagree about what a show contains.
        const ids = await queueFor(qc, item);
        for (let i = 0; i < ids.length; i += 8) {
          await Promise.all(
            ids
              .slice(i, i + 8)
              .map((id) =>
                setWatched.mutateAsync({ itemID: id, watched }).catch(() => {}),
              ),
          );
        }
        /*
         * And then the container's own view of itself.
         *
         * useSetWatchedByID invalidates the item it wrote and the lists a tile
         * lives in. That is right for one write and not enough for this one:
         * the show whose episodes just changed was never written to, so nothing
         * invalidated its children list or its own row. The symptom is this
         * project's most-repeated bug — the request succeeds, the server is
         * right, and the season page you are looking at still draws every
         * episode unwatched.
         */
        qc.invalidateQueries({ queryKey: ["children"] });
        qc.invalidateQueries({ queryKey: ["item", item.id] });
      } finally {
        setGathering(false);
      }
    },
    [gathering, qc, setWatched],
  );

  /*
   * The sensitive gesture (ADR 0051).
   *
   * Offered on pictures only, and only while the setting is on. Two conditions
   * rather than one: the setting decides whether the feature exists at all, and
   * a library of films is not what it was built for.
   *
   * Unmark appears only where the mark actually is. A photograph inside a
   * marked folder reads sensitive and *not* sensitive_own, and offering it an
   * Unmark that would do nothing — because the folder above it is still marked
   * — is the kind of control that teaches people the feature is broken.
   */
  const { data: settings } = useSettings();
  const setSensitive = useSetSensitive();
  const sensitiveActions = useCallback(
    (item: Item): MenuAction[] => {
      if (!isAdmin || !settings?.sensitive_marking) return [];
      if (!isPicture(item)) return [];
      if (item.sensitive_own) {
        return [
          {
            label: "Not sensitive",
            onSelect: () =>
              setSensitive.mutate({ id: item.id, sensitive: false }),
          },
        ];
      }
      // Covered by a folder above, and nothing to do about it here.
      if (item.sensitive) return [];
      return [
        {
          label: "Mark sensitive",
          onSelect: () => setSensitive.mutate({ id: item.id, sensitive: true }),
        },
      ];
    },
    [isAdmin, settings?.sensitive_marking, setSensitive],
  );

  const actions = useCallback(
    (item: Item): MenuAction[] => {
      /*
       * Removal is admin-only, matching the track row that already offers it.
       * The dialog behind it is the same one the detail page uses, so the two
       * ways of removing a title cannot come to disagree about what "remove"
       * means — one keeps the file and ignores it, the other deletes it.
       */
      const remove: MenuAction[] = isAdmin
        ? [
            {
              label: "Remove from library…",
              danger: true,
              onSelect: () => setRemoveFor(item),
            },
          ]
        : [];

      /*
       * A photograph, and the gallery holding it.
       *
       * Neither is watched, queued or played, and a photo has no detail page
       * worth visiting — the reason a photo tile selects itself into the banner
       * above rather than navigating. What is left is small and was completely
       * unreachable: a mis-scanned folder of camera exports could only be
       * removed from the detail page of a thing with no detail page.
       */
      if (isPicture(item)) {
        const sensitive = sensitiveActions(item);
        return item.kind === "gallery"
          ? [
              {
                label: "Go to details",
                onSelect: () => navigate(`/item/${item.id}`),
              },
              ...sensitive,
              ...remove,
            ]
          : [...sensitive, ...remove];
      }

      if (isContainer(item)) {
        /*
         * A collection's membership is many-to-many through item_collection,
         * and a playlist's runs through playlist_entry — neither has children
         * under parent_id, so neither has a queue this can build. Their pages
         * know how. A tile does not, and a Play all that silently queues
         * nothing is worse than no Play all.
         */
        const playable = item.kind !== "collection" && item.kind !== "playlist";
        const verb = watchedVerb(item);
        return [
          ...(playable
            ? [
                {
                  label: gathering ? "Gathering…" : "Play all",
                  disabled: gathering,
                  onSelect: () => void playContainer(item, false),
                },
                {
                  label: "Shuffle",
                  disabled: gathering,
                  onSelect: () => void playContainer(item, true),
                },
                {
                  label: `Mark all as ${verb.past}`,
                  disabled: gathering,
                  onSelect: () => void markAll(item, true),
                },
                {
                  label: `Mark all as ${verb.negated}`,
                  disabled: gathering,
                  onSelect: () => void markAll(item, false),
                },
              ]
            : []),
          {
            label: "Go to details",
            onSelect: () => navigate(`/item/${item.id}`),
          },
          ...remove,
        ];
      }

      const verb = watchedVerb(item);
      const seen = item.progress?.watched ?? false;
      /*
       * "Play" resumes, and there was no way to say otherwise.
       *
       * A half-watched film's tile carries a progress bar, so the one thing
       * somebody right-clicking *that particular tile* might mean — start it
       * again from the top — was the one thing this menu could not do. The
       * second entry appears only when there is a position to ignore, so an
       * untouched film still shows a single unambiguous Play rather than two
       * verbs that mean the same thing.
       */
      const started = (item.progress?.position_ms ?? 0) > 0 && !seen;
      return [
        {
          label: started ? "Resume" : "Play",
          onSelect: () => navigate(`/watch/${item.id}`),
        },
        ...(started
          ? [
              {
                /*
                 * Starting over *is* discarding the resume point.
                 *
                 * The tempting version passes a `restart` flag through the
                 * route into the provider, which already decides where to begin
                 * from `item.progress` — a second source of truth for where a
                 * film starts, and the two would disagree the first time
                 * anything else navigated to it. Forgetting the position
                 * instead needs no new mechanism, says the same thing to every
                 * client, and is what somebody picking this actually means.
                 *
                 * Awaited, or the navigation races the invalidation and the
                 * provider reads the position it is being told to forget.
                 */
                label: "Play from start",
                onSelect: () =>
                  void setWatched
                    .mutateAsync({ itemID: item.id, watched: false })
                    .finally(() => navigate(`/watch/${item.id}`)),
              },
            ]
          : []),
        {
          label: seen ? `Mark as ${verb.negated}` : `Mark as ${verb.past}`,
          onSelect: () => setWatched.mutate({ itemID: item.id, watched: !seen }),
        },
        { label: "Play next", onSelect: () => pb.playNextUp(item.id) },
        { label: "Add to queue", onSelect: () => pb.addToQueue(item.id) },
        /*
         * Playlists are a music format. An .m3u seeds one (ADR 0030) and every
         * playlist anybody has made holds tracks, so offering this on a film
         * would be offering to build a thing the rest of the client has no way
         * to play back. The track row has always had it; a track's *poster*
         * never did, which is the same capability shipped half-reachable.
         */
        ...(isMusic(item)
          ? [
              {
                label: "Add to playlist…",
                onSelect: () => setPlaylistFor(item),
              },
            ]
          : []),
        { label: "Go to details", onSelect: () => navigate(`/item/${item.id}`) },
        ...remove,
      ];
    },
    [
      navigate,
      setWatched,
      pb,
      isAdmin,
      gathering,
      playContainer,
      markAll,
      sensitiveActions,
    ],
  );

  const dialogs = (
    <>
      {playlistFor && (
        <AddToPlaylist item={playlistFor} onClose={() => setPlaylistFor(null)} />
      )}
      {removeFor && (
        <RemoveDialog
          item={removeFor}
          onClose={() => setRemoveFor(null)}
          // A tile is not a page, so there is nowhere to navigate away to.
          // useDeleteItem invalidates the lists the tile lived in, and that is
          // what makes it leave the grid.
          onDone={() => setRemoveFor(null)}
        />
      )}
    </>
  );

  return { actions, dialogs };
}
