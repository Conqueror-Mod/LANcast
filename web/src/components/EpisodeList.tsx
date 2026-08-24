import { useCallback, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useFocusable } from "@/focus/FocusController";
import { useIsAdmin, useSetWatched } from "@/api/hooks";
import { usePlayback } from "@/playback/PlaybackProvider";
import { PointMenu, type MenuAction, type MenuPoint } from "./Menu";
import { RemoveDialog } from "./RemoveDialog";
import { watchedVerb } from "@/lib/kind";
import { artworkURL } from "@/api/client";
import { runtime } from "@/lib/format";
import { spoilerState, useSpoilerMode } from "@/lib/spoilers";
import type { Item } from "@/api/types";
import "./EpisodeList.css";

/*
 * A season is a list of episodes, not a grid of posters.
 *
 * The screen this replaces drew episodes with the movie grid: 2:3 tiles, a
 * title underneath, and nothing else. An episode is not a poster — it is a
 * 16:9 still with a synopsis, a runtime and a place in an order — so the tiles
 * were tall, empty, and had no room for the one thing a season page exists to
 * show. The same correction the music arc already made for albums
 * (season-page-plan.md, and music-client-plan.md before it).
 *
 * Deliberately built before episode artwork exists. Every row renders its
 * number in the space a still will occupy, which is a state rather than a
 * placeholder, and the rows fill in later without the layout changing.
 */
export function EpisodeList({
  episodes,
  queue,
  parentID,
}: {
  episodes: Item[];
  /** Playing an episode queues the rest of the season from it, so a season
   *  keeps playing after the row that was pressed. */
  queue: number[];
  /** The container being listed, so marking an episode watched can refresh the
   *  list it is in. */
  parentID: number;
}) {
  const setWatched = useSetWatched(parentID);
  const isAdmin = useIsAdmin();
  /*
   * One dialog for the list, not one per row.
   *
   * Only one can be open, and a RemoveDialog mounted per episode would be
   * twenty-six dialogs on a season -- the same call TrackList makes, for the
   * same reason. Removing an episode was already permitted by the server and
   * was reachable from nowhere: nothing in the client navigates to an episode's
   * own page, so the one screen that offers removal could not be got to.
   */
  const [removing, setRemoving] = useState<Item | null>(null);
  // Read once for the list rather than per row: it is one device setting, and a
  // hook per row would read the same value twenty-six times.
  const [spoilerMode] = useSpoilerMode();
  if (episodes.length === 0) return null;
  return (
    <div className="eplist">
      {episodes.map((ep) => (
        <EpisodeRow
          key={ep.id}
          episode={ep}
          queue={queue}
          spoilers={spoilerState(spoilerMode, ep)}
          onSetWatched={(watched) =>
            setWatched.mutate({ itemID: ep.id, watched })
          }
          onRemove={isAdmin ? setRemoving : undefined}
        />
      ))}
      {removing && (
        <RemoveDialog
          item={removing}
          onClose={() => setRemoving(null)}
          // Nowhere to navigate: the season page outlives the episode, and
          // useDeleteItem invalidates the children list this is drawn from.
          onDone={() => setRemoving(null)}
        />
      )}
    </div>
  );
}

function EpisodeRow({
  episode,
  queue,
  spoilers,
  onSetWatched,
  onRemove,
}: {
  episode: Item;
  queue: number[];
  spoilers: { hideSynopsis: boolean; hideStill: boolean };
  onSetWatched: (watched: boolean) => void;
  /** Absent for anyone who is not an admin, which is what hides the item. */
  onRemove?: (episode: Item) => void;
}) {
  const navigate = useNavigate();
  const play = () =>
    navigate(`/watch/${episode.id}`, { state: { queue } });
  const pb = usePlayback();
  const [menuAt, setMenuAt] = useState<MenuPoint | null>(null);
  const [byKey, setByKey] = useState(false);
  const openedFrom = useRef<HTMLElement | null>(null);
  // Anchored under the row, and registered through the controller so the key is
  // the one in the keyboard settings rather than one this row invented.
  const openMenu = useCallback((el: HTMLElement) => {
    openedFrom.current = el;
    setByKey(true);
    const at = el.getBoundingClientRect();
    setMenuAt({ x: at.left, y: at.bottom });
  }, []);
  // Through the focus controller like every other interactive element, so
  // spatial navigation covers a season list without a second idea of what
  // focus means (ADR 0004).
  const focusable = useFocusable(play, openMenu);
  const watched = !!episode.progress?.watched;
  // The mark control opens the same menu, so the actions key works wherever on
  // the row focus happens to be.
  const markFocusable = useFocusable(() => onSetWatched(!watched), openMenu);

  const progress = episode.progress;
  const duration = episode.duration_ms ?? 0;
  /*
   * A bar only when there is something to say.
   *
   * An untouched season should not be a wall of empty bars, and a finished one
   * should not be a wall of full ones — watched is said by the row's own state,
   * not by a bar sitting at 100%.
   */
  const pct =
    progress && !progress.watched && duration > 0
      ? Math.min(100, Math.round((progress.position_ms / duration) * 100))
      : 0;
  const left = pct > 0 ? runtime(duration - (progress?.position_ms ?? 0)) : "";

  /*
   * The row is a container holding two controls, not one button holding
   * another. A button inside a button is invalid, and the browser's recovery
   * from it is to drop one — so the mark control sits beside the play area
   * rather than inside it.
   */
  /*
   * The row's menu, and what it deliberately does not take away.
   *
   * Same contract as a track row: it only *adds*. The mark-watched control
   * stays a button beside the play area, because this list is driven by a
   * remote and a control that only exists inside a menu is one a d-pad has to
   * be told about — the keyboard route makes that reachable now, but a working
   * button is not worth removing to prove it.
   *
   * "Go to details" is here because nothing else in the client navigates to an
   * episode's own page. It is reachable and has always been unreachable, which
   * is the same shape as the two capabilities TrackList records as having
   * shipped and never been usable.
   */
  const actions: MenuAction[] = [
    /*
     * Pressing the row resumes, and there was no way to say otherwise -- the
     * one thing the bar under a half-watched episode invites you to want.
     * Starting over is spelled as forgetting the position rather than as a flag
     * carried into the player, because the player already decides where to
     * begin from `progress`: a second source of truth for where an episode
     * starts would disagree the first time anything else navigated to it.
     *
     * Only where there is a position to ignore. On a fresh episode "Play" and
     * "Play from start" are two verbs for one action.
     */
    { label: pct > 0 ? "Resume" : "Play", onSelect: play },
    ...(pct > 0
      ? [
          {
            label: "Play from start",
            onSelect: () => {
              onSetWatched(false);
              play();
            },
          },
        ]
      : []),
    {
      label: watched
        ? `Mark as ${watchedVerb(episode).negated}`
        : `Mark as ${watchedVerb(episode).past}`,
      onSelect: () => onSetWatched(!watched),
    },
    { label: "Play next", onSelect: () => pb.playNextUp(episode.id) },
    { label: "Add to queue", onSelect: () => pb.addToQueue(episode.id) },
    {
      label: "Go to details",
      onSelect: () => navigate(`/item/${episode.id}`),
    },
    ...(onRemove
      ? [
          {
            label: "Remove from library…",
            danger: true,
            onSelect: () => onRemove(episode),
          },
        ]
      : []),
  ];

  return (
    <div
      className={"eprow" + (watched ? " eprow--watched" : "")}
      onContextMenu={(e) => {
        e.preventDefault();
        setByKey(false);
        setMenuAt({ x: e.clientX, y: e.clientY });
      }}
    >
      <button
        {...focusable}
        className="eprow__play"
        onClick={play}
        // The row is the play control, so it says so. "4 · I, Roommate" read on
        // its own does not tell a screen-reader user that pressing it plays.
        aria-label={`Play ${episode.episode ? `episode ${episode.episode}, ` : ""}${episode.title}`}
      >
        <EpisodeStill episode={episode} hidden={spoilers.hideStill} />

        <span className="eprow__body">
          <span className="eprow__head">
            <span className="eprow__title">
              {episode.episode != null && (
                <span className="eprow__num">{episode.episode}</span>
              )}
              {episode.title}
            </span>
            <span className="eprow__meta">
              {duration > 0 && <span>{runtime(duration)}</span>}
              {episode.released_at ? (
                <span>{airDate(episode.released_at)}</span>
              ) : null}
              {episode.rating ? <span>★ {episode.rating.toFixed(1)}</span> : null}
            </span>
          </span>

          {/* The reason a season page exists at all: the grid had nowhere to put
              this. Clamped to two lines so a long synopsis cannot make one row
              taller than the three around it. */}
          {/*
             * A synopsis, or a line saying why there is not one.
             *
             * Silence would read as missing metadata, which is the failure this
             * whole screen was built to stop looking like. Saying it is hidden
             * on purpose is shorter than a synopsis and answers the question
             * somebody would otherwise ask.
             */}
          {spoilers.hideSynopsis ? (
            <span className="eprow__synopsis eprow__synopsis--hidden">
              Synopsis hidden until watched
            </span>
          ) : (
            episode.overview && (
              <span className="eprow__synopsis">{episode.overview}</span>
            )
          )}

          {pct > 0 && (
            <span className="eprow__progress">
              <span className="eprow__bar">
                <span className="eprow__bar-fill" style={{ width: `${pct}%` }} />
              </span>
              <span className="eprow__left">{left} left</span>
            </span>
          )}
        </span>
      </button>

      {/*
       * Watched, and the way back from it.
       *
       * The tick was a label; it is a control now, because a season page is
       * where somebody corrects this — an episode watched on the television,
       * or one the player marked finished while it walked a queue. Marking
       * unwatched sends a position of zero as well, since leaving the position
       * behind would put the row straight back on the Continue shelf.
       */}
      <button
        {...markFocusable}
        className={"eprow__mark" + (watched ? " is-on" : "")}
        onClick={() => onSetWatched(!watched)}
        aria-pressed={watched}
        aria-label={
          watched
            ? `Mark ${episode.title} unwatched`
            : `Mark ${episode.title} watched`
        }
        title={watched ? "Mark unwatched" : "Mark watched"}
      >
        ✓
      </button>
      {menuAt && (
        <PointMenu
          at={menuAt}
          actions={actions}
          autoFocus={byKey}
          onClose={() => {
            setMenuAt(null);
            // Back to the row, or a remote is left with nothing focused.
            if (byKey) openedFrom.current?.focus();
          }}
        />
      )}
    </div>
  );
}

/*
 * The still, or the number where the still would be.
 *
 * Not a grey rectangle and not the show's poster squeezed into a wide frame: a
 * missing image drawn as a missing image reads as broken, while a number drawn
 * as a number reads as a design. It is also why this screen could ship before
 * episode artwork is stored at all — today every row takes this path.
 */
function EpisodeStill({
  episode,
  hidden,
}: {
  episode: Item;
  /** Spoiler protection at its strongest setting: the still is withheld and the
   *  number takes its place, which is the state a missing still already uses —
   *  so hiding one costs no new design. */
  hidden?: boolean;
}) {
  /*
   * A hash is not a URL.
   *
   * The item's artwork fields carry content-addressed hashes, and every other
   * screen turns them into URLs through artworkURL. The first version of this
   * row put the hash straight into `src`, which is worse than having no image:
   * the value is truthy, so the row took the image branch and rendered a broken
   * one instead of the number it falls back to.
   *
   * `poster` (342px) rather than `thumb` (185px) because the still box is 200px
   * wide and 185 is soft on a high-density display — the size names describe
   * widths, not roles.
   */
  const still = hidden ? undefined : artworkURL(episode.artwork?.thumb, "poster");
  if (still) {
    return (
      <span className="eprow__still">
        <img src={still} alt="" loading="lazy" />
      </span>
    );
  }
  return (
    <span className="eprow__still eprow__still--empty" aria-hidden="true">
      <span className="eprow__stillnum">{episode.episode ?? "•"}</span>
    </span>
  );
}

// The air date as a date, in the viewer's own locale rather than an ISO string:
// this is the one place a person reads it, and 1999-04-13 is a machine's idea of
// a date. Built from local components for the reason every date here is.
function airDate(unix: number): string {
  const d = new Date(unix * 1000);
  return d.toLocaleDateString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
  });
}
