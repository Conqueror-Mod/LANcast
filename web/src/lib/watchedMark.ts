import { isMusic, isPicture } from "./kind";
import type { Item } from "@/api/types";

/*
 * Whether a tile should carry a finished-tick.
 *
 * A function rather than an expression inside PosterTile because the answer is
 * different for three kinds of row and identical-looking in all three, which is
 * exactly the shape of thing that gets copied to a second surface and then
 * quietly disagrees with the first.
 *
 * Two readings of "watched" meet here:
 *
 *   a leaf answers for itself       — progress.watched, the server's own flag
 *   a show or season answers by aggregate — unwatched_episodes === 0
 *
 * They cannot be collapsed. A show has no playback row of its own, so reading
 * `progress` on one is always undefined, and a film has no episodes, so
 * reading `unwatched_episodes` on one is always undefined. Asking the wrong
 * question of either gives "not watched" — silently, and for ever.
 */
export function isWatched(item: Item): boolean {
  /*
   * Music and photographs are out, and this is a decision rather than an
   * oversight.
   *
   * A track carries the same `watched` flag a film does — the server is right
   * to store one column — but the client already decided how a finished track
   * reads: TrackList dims the title, because a checkmark against every row of
   * a fifteen-track album is noise. Putting a tick on album and track tiles
   * would be the same signal said twice, in two different visual languages.
   *
   * A photograph is simpler: nothing marks one as watched, so the tick would
   * be permanently absent and the check is here to say so on purpose.
   */
  if (isMusic(item) || isPicture(item)) return false;

  /*
   * A show is finished when it has no episodes left.
   *
   * `=== 0` and not `!unwatched_episodes`, because the falsy test is the bug:
   * the field is **absent** on a film, on an album, and on a show whose
   * episodes are not on disk, and `!undefined` is true. That reading would tick
   * every tile in the library, which is a failure loud enough to be caught —
   * unlike its mirror image, where a show that really is finished is missing
   * the one mark somebody is looking for.
   */
  if (isSeries(item)) return item.unwatched_episodes === 0;

  /*
   * A collection is finished when every film in it is. Absent progress (no
   * films on disk) is not finished, for the reason an empty series is not.
   */
  if (item.kind === "collection") {
    const p = item.collection_progress;
    return !!p && p.total > 0 && p.watched >= p.total;
  }

  // Everything else — a film, an episode, a part — carries its own flag.
  return item.progress?.watched === true;
}

/*
 * What the tick's label says.
 *
 * A show says how it got there. "Watched" on a series is ambiguous in a way it
 * is not on a film — it could mean one episode — and the tick is the only thing
 * on the tile that answers, so the accessible name is where the answer goes.
 */
export function watchedLabel(item: Item): string {
  if (item.kind === "collection") return "Every film watched";
  return isSeries(item) ? "Every episode watched" : "Watched";
}

/*
 * A collection somebody has begun and not finished.
 *
 * The two numbers the tile draws from: how much of the bar to fill, and the
 * "2 of 5 watched" that replaces "5 films" under the title. Null for anything
 * else, and for a collection untouched or finished — an untouched one keeps
 * its film count, and a finished one has the tick.
 */
export function collectionStanding(
  item: Item,
): { pct: number; label: string } | null {
  if (item.kind !== "collection") return null;
  const p = item.collection_progress;
  if (!p || p.total <= 0) return null;
  if (p.watched >= p.total) return null;
  if (p.watched === 0 && p.in_progress === 0) return null;
  /*
   * A film part way counts as half of one, so a box set begun with nothing
   * finished still shows a bar rather than an empty one that reads as
   * untouched. Never full: full is the tick's job.
   */
  const pct = Math.min(95, ((p.watched + p.in_progress * 0.5) / p.total) * 100);
  return { pct, label: `${p.watched} of ${p.total} watched` };
}

/*
 * A show or a season: a container of episodes with no play state of its own,
 * which answers by aggregate. A season card on a show page is the same
 * question one level down, and was missing its tick until the server counted
 * seasons too.
 */
function isSeries(item: Item): boolean {
  return item.kind === "show" || item.kind === "season";
}
