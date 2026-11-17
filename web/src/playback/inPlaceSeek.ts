/*
 * When a seek on a converted stream can stay inside the stream it is playing.
 *
 * A seek while converting re-requests the stream at a new `t=`, which is a new
 * session on the server and a new playlist. For a picture the server re-encodes
 * that is the only option: its playlist is listed before the segments exist,
 * and a segment far ahead is produced only when the encoder reaches it
 * (internal/transcode/vodplaylist.go).
 *
 * A **copied** picture is the opposite case. The server holds the playlist
 * until the remux has finished (internal/api/remuxpatience.go), so by the time
 * the element has a finished playlist, every segment from the offset to the end
 * of the film is on disk. Re-requesting threw all of that away and paid for the
 * remux again: 24–25 s on a 22-minute episode, about a minute on a film. It
 * happened on every seek, and a Watch Together follower seeks to keep up, so it
 * restarted four times in ninety seconds before it settled (ledger row 6,
 * 2026-11-16). With a start-up longer than the follower's largest lead, a film
 * might never have settled at all.
 *
 * So a seek stays in place when all of these hold:
 *   - the stream is a playlist (not the progressive response, which is one
 *     endless body with nothing ahead to seek to);
 *   - its picture is copied;
 *   - the element has a finite duration. A finished playlist ends with
 *     ENDLIST; one served growing, because the remux had not finished, plays
 *     as live with an infinite duration and has nothing ahead yet;
 *   - the target is inside it, from the offset to just short of its end.
 *
 * Anything else, including a target before the offset, re-requests as before.
 */

// Short of the very end, so a seek to the last instant of the stream is not
// left waiting on a final fragment the element may already have run out of.
export const IN_PLACE_END_MARGIN_S = 1;

export interface InPlaceSeek {
  /** The delivery path: "hls", "progressive" or "direct". */
  path: string;
  /** The decision's video_action: "copy" or "encode", absent when unknown. */
  videoAction?: string;
  /** Where in the film the current stream begins, in seconds. */
  offset: number;
  /** The element's duration, in seconds: Infinity or NaN while not finished. */
  duration: number;
  /** Where in the film the seek is to, in seconds. */
  target: number;
}

export function seeksInPlace(s: InPlaceSeek): boolean {
  if (s.path !== "hls" || s.videoAction !== "copy") return false;
  if (!Number.isFinite(s.duration) || s.duration <= 0) return false;
  return s.target >= s.offset && s.target <= s.offset + s.duration - IN_PLACE_END_MARGIN_S;
}
