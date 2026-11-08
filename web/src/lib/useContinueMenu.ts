import { useNavigate } from "react-router-dom";
import { useSetWatchedByID } from "@/api/hooks";
import { usePlayback } from "@/playback/PlaybackProvider";
import type { MenuAction } from "@/components/Menu";
import { useMarkAll } from "@/components/itemActions";
import { watchedVerb } from "@/lib/kind";
import { showContinueTarget } from "@/lib/continueShow";
import { resumeTarget } from "@/lib/resume";
import type { Item, ResumeCard } from "@/api/types";

/*
 * What a Continue entry's menu offers and what pressing one does.
 *
 * Shared by the home page's resume console and its See all page (ADR 0118),
 * and by the spotlight's More Options when its title is in progress. One copy,
 * because two menus for the same entry that disagree about what Remove does
 * is the bug this menu's history is made of.
 */
export function useContinueMenu() {
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
   * writes serve an album track and a film, but "Mark as watched" on a song is
   * the interface reading from the wrong half of itself — small, and exactly
   * the kind of small that makes a feature feel bolted on. The Remove entry
   * says "Continue" for every kind, because since ADR 0118 it is one list.
   */
  const { markAll, marking } = useMarkAll();

  const continueActions = (item: Item): MenuAction[] => {
    /*
     * A show in progress is marked through its episodes.
     *
     * The show row has no play state of its own: the shelf decides a show's
     * place from what its episodes say. These menu items used to write to the
     * show row, which the server accepted and the shelf ignored — "Mark as
     * watched" and "Remove" both reported success and left the show where it
     * was. The grid's container menu already marks every episode; this is the
     * same code.
     *
     * There is no "Remove" here, deliberately. A show is on the shelf because
     * of any episode activity, finished episodes included, and the only way to
     * take it off without claiming it was all watched is to forget those
     * episodes too. That is what "Mark all as unwatched" says out loud, where
     * a "Remove" would have done it quietly.
     */
    if (item.kind === "show") {
      return [
        {
          label: "Mark all as watched",
          disabled: marking,
          onSelect: () => void markAll(item, true),
        },
        {
          label: "Mark all as unwatched",
          disabled: marking,
          onSelect: () => void markAll(item, false),
        },
        {
          label: "Go to details",
          onSelect: () => navigate(`/item/${item.id}`),
        },
      ];
    }
    return [
      {
        label: `Mark as ${watchedVerb(item).past}`,
        onSelect: () => setWatched.mutate({ itemID: item.id, watched: true }),
      },
      {
        // The console is one list, so the entry names it once for every kind
        // (ADR 0118). It still claims nothing about whether it was seen.
        label: "Remove from Continue",
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
    /*
     * A collection's next film opens its page knowing which collection it
     * continues, so Play there carries on through the franchise rather than
     * stopping after the one film (Detail's continueLeaf).
     */
    if (item.kind === "movie" && item.continues_from) {
      const collectionID = item.continues_from.collection_id;
      return () => navigate(`/item/${item.id}`, { state: { collectionID } });
    }
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

  /*
   * A card's menu. Most kinds take the Continue menu as it was, written to
   * the thing that holds the place -- an album card's track, not the album.
   *
   * A comic and a game do not. A comic is listed while its page is saved and
   * a game while it has a save, so a "Remove" that cleared a play position
   * would report success and leave the card where it was. A comic leaves by
   * being read; a game never leaves by itself, and offers only its page.
   */
  const resumeActions = (card: ResumeCard): MenuAction[] => {
    const details = { label: "Go to details", onSelect: () => navigate(`/item/${card.item.id}`) };
    if (card.kind === "game") return [details];
    if (card.kind === "comic") {
      return [
        {
          label: `Mark as ${watchedVerb(card.item).past}`,
          onSelect: () => setWatched.mutate({ itemID: card.item.id, watched: true }),
        },
        details,
      ];
    }
    return continueActions(resumeTarget(card));
  };
  // Pressing a film or a show does what its shelf tile did; everything else
  // follows the card's own target.
  const resumeOpen = (card: ResumeCard) =>
    card.kind === "video" || card.kind === "show" ? continueOpen(card.item) : undefined;

  return { continueActions, continueOpen, resumeActions, resumeOpen };
}
