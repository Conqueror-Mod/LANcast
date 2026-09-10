/*
 * What the player says when the server will not start playback.
 *
 * # Why this is a function and not a string in the provider
 *
 * Because it was a string in the provider, and it named a cause it could not
 * know: "the server may already be converting as much as it can". That is one
 * refusal among several — a file no longer on disk, a path that fails the
 * containment check, ffmpeg not installed at all — and it was stated as though
 * it were the finding, so every failure on this path pointed at load.
 *
 * It cost a real investigation. A show whose path is the series directory was
 * handed to the player, the server refused it at the containment check
 * (correctly — there is no file there), and this text sent everybody to look at
 * how busy the server was. The server was idle.
 *
 * # Why it cannot simply find out
 *
 * A `<video>` element reports MEDIA_ERR codes and nothing else, so the status
 * the server sent is not visible from here. Asking again would start the very
 * transcode that was refused, taking a slot in order to explain why there were
 * no slots, and there is no side-effect-free endpoint that answers "why can
 * this not play". So the honest position is that the request was refused, that
 * waiting will not help, and where the answer actually is.
 */

/** What the player knows about the item without asking anything. */
export interface RefusedContext {
  /** The server has marked the file absent. */
  missing: boolean;
}

export function refusedNote({ missing }: RefusedContext): string {
  /*
   * A known-missing file is a certainty, not one of several possibilities.
   *
   * `missing` is already on the payload in hand, so this costs no request and
   * starts no transcode. Scanning marks missing rather than deleting, which is
   * why the way back is a scan and not a re-import — the library is intact and
   * waiting for the drive.
   */
  if (missing) {
    return (
      "This file is not on the server any more. It may be on a drive that is " +
      "disconnected — a scan will update the library once it is back."
    );
  }

  return (
    "The server refused to start playback. It may be busy converting — " +
    "Activity in Settings shows what — or unable to read the file. The " +
    "server log records which."
  );
}
