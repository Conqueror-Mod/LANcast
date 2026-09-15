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
 *   a show answers by aggregate     — unwatched_episodes === 0
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
  if (item.kind === "show") return item.unwatched_episodes === 0;

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
  return item.kind === "show" ? "Every episode watched" : "Watched";
}
