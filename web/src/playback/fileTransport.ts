import { readDevice, writeDevice } from "@/lib/device";
import { HLS_MIME } from "@/lib/liveTransport";

/*
 * How a converted file reaches the element: one endless response, or segments.
 *
 * # The bug this exists for
 *
 * A progressive transcode is one fMP4 response of unknown length, and the
 * handler says so honestly — `Accept-Ranges: none`, because bytes that ffmpeg
 * has not produced yet cannot be range-served. The comment there claimed that
 * saying so "stops browsers issuing range requests that could never be
 * satisfied". The log disagrees.
 *
 * `All About the Benjamins` — 5.4 Mbps, video copied, audio re-encoded — logged
 * **twelve transcode sessions in eighteen minutes, every one at `start_at=0`**,
 * with no ffmpeg error and nothing reaped. Chromium caps how much media it will
 * hold; on a stream that dense the cap is a few minutes. When it evicts and
 * needs those bytes again it cannot ask for a range, so it drops the connection
 * and starts the whole film over from byte zero. The further in you are, the
 * more there is to re-stream before the picture moves again — reported as
 * lagging every few minutes, starting about fifteen minutes in.
 *
 * Segments fix it at the root rather than papering over it: evicting one
 * segment costs one segment, and the element re-asks for exactly that.
 *
 * # Why not hls.js
 *
 * ADR 0013 declined to vendor ~300KB of unaudited third-party library, and this
 * does not reopen that. Measured on Chrome 148 against a real VOD playlist: the
 * element played it from `src` with no library at all — `readyState` 4, both
 * tracks decoding — and, the part that matters here, reported a real seekable
 * range of 0–30.05s with a forward seek landing in 52ms and a backward seek in
 * 35ms. Native, seekable, no dependency.
 *
 * # Why this is learned rather than asked
 *
 * `canPlayType('application/vnd.apple.mpegurl')` answers **"maybe"** on
 * Chromium, and it answers "maybe" whether or not playback will actually work.
 * It is worth exactly nothing as a gate: it cannot separate the engine that
 * plays HLS from the one that will show a black rectangle, and the engines this
 * project meets — WebView2 on whatever runtime is installed, television
 * browsers — are precisely the ones where the answer differs from the desktop
 * Chrome it was measured on.
 *
 * So the capability is *discovered by trying it*, once, and remembered per
 * device. An element that rejects the playlist outright is the only reliable
 * evidence there is, and it costs one failed load in the life of a device.
 * Guessing from a string that means "maybe" would be the same class of mistake
 * as the comment that started this: a claim about an engine, asserted rather
 * than watched.
 */

/** Where the source URL points. */
export type FilePath = "direct" | "hls" | "progressive";

/**
 * What this device has been observed to do with a playlist.
 *
 * `unknown` means nobody has tried yet, and is the state that makes HLS get
 * attempted at all.
 */
export type HLSVerdict = "unknown" | "playable" | "refused";

export const HLS_VERDICT_KEY = "lancast:hls-playable";

export function hlsVerdict(): HLSVerdict {
  return readDevice<HLSVerdict>(HLS_VERDICT_KEY, "unknown");
}

/**
 * rememberHLS records what happened when this device was handed a playlist.
 *
 * Written once and read for ever after: a device that refused a playlist is not
 * asked again on the next film, because the cost of asking is a visible failed
 * load and the answer does not change.
 */
export function rememberHLS(v: Exclude<HLSVerdict, "unknown">): void {
  writeDevice(HLS_VERDICT_KEY, v);
}

/**
 * Whether a playlist is worth attempting on this device.
 *
 * `canPlayType` is consulted only to rule the path out: an engine that answers
 * with the empty string is saying it has no idea what a playlist is, and that
 * answer *is* trustworthy — it is only "maybe" that means nothing. Everything
 * else is settled by trying.
 */
export function hlsWorthTrying(
  canPlayType: (t: string) => string,
  verdict: HLSVerdict = hlsVerdict(),
): boolean {
  if (verdict === "refused") return false;
  if (verdict === "playable") return true;
  return canPlayType(HLS_MIME) !== "";
}

/**
 * filePath chooses how a file is delivered.
 *
 * Direct play is never touched. Those are the file's own bytes over a range
 * server: already seekable, already resumable, and nothing here improves them —
 * the eviction problem is a property of a stream that cannot be re-asked, and a
 * real file can be.
 */
export function filePath(
  method: string,
  hlsUsable: boolean,
): FilePath {
  if (method === "direct") return "direct";
  return hlsUsable ? "hls" : "progressive";
}

/**
 * Whether a media error means "this engine cannot play a playlist".
 *
 * Narrow on purpose. `MEDIA_ERR_SRC_NOT_SUPPORTED` is the element saying it
 * could not make sense of the resource at all, which is what an engine without
 * HLS does with a playlist. A decode error or a network error is a statement
 * about *this file* or *this moment*, and treating either as a verdict on the
 * device would retire the better path for ever over one bad transcode or one
 * dropped connection.
 */
export function isUnsupportedSource(err: MediaError | null): boolean {
  return !!err && err.code === 4; // MEDIA_ERR_SRC_NOT_SUPPORTED
}
