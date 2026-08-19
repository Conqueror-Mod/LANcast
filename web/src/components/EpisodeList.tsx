import { useNavigate } from "react-router-dom";
import { useFocusable } from "@/focus/FocusController";
import { runtime } from "@/lib/format";
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
}: {
  episodes: Item[];
  /** Playing an episode queues the rest of the season from it, so a season
   *  keeps playing after the row that was pressed. */
  queue: number[];
}) {
  if (episodes.length === 0) return null;
  return (
    <div className="eplist">
      {episodes.map((ep) => (
        <EpisodeRow key={ep.id} episode={ep} queue={queue} />
      ))}
    </div>
  );
}

function EpisodeRow({ episode, queue }: { episode: Item; queue: number[] }) {
  const navigate = useNavigate();
  const play = () =>
    navigate(`/watch/${episode.id}`, { state: { queue } });
  // Through the focus controller like every other interactive element, so
  // spatial navigation covers a season list without a second idea of what
  // focus means (ADR 0004).
  const focusable = useFocusable(play);

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

  return (
    <button
      {...focusable}
      className={"eprow" + (progress?.watched ? " eprow--watched" : "")}
      onClick={play}
      // The row is the play control, so it says so. "4 · I, Roommate" read on
      // its own does not tell a screen-reader user that pressing it plays.
      aria-label={`Play ${episode.episode ? `episode ${episode.episode}, ` : ""}${episode.title}`}
    >
      <EpisodeStill episode={episode} />

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
            {episode.released_at ? <span>{airDate(episode.released_at)}</span> : null}
            {episode.rating ? <span>★ {episode.rating.toFixed(1)}</span> : null}
          </span>
        </span>

        {/* The reason a season page exists at all: the grid had nowhere to put
            this. Clamped to two lines so a long synopsis cannot make one row
            taller than the three around it. */}
        {episode.overview && (
          <span className="eprow__synopsis">{episode.overview}</span>
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

      {progress?.watched && (
        <span className="eprow__watched" aria-label="Watched">
          ✓
        </span>
      )}
    </button>
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
function EpisodeStill({ episode }: { episode: Item }) {
  const still = episode.artwork?.thumb;
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
