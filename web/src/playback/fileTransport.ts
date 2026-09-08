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

/*
 * Versioned, and the version is a repair.
 *
 * The first key recorded a verdict that could be wrong for a reason nothing
 * here could see — a server that failed to produce a playlist looked exactly
 * like an engine that could not read one, so one bad thirty-second wait retired
 * the better path on that device for ever. Observed doing precisely that: one
 * `hls playlist unavailable` on 31 August, and every file played since went
 * down the progressive path with the eviction fault this whole module exists to
 * avoid.
 *
 * Bumping the key is how those devices get a second opinion. A verdict written
 * under the old rules is not worth migrating — it was reached by a test that
 * could not tell the two failures apart — so it is left behind rather than
 * read, and each device pays one attempt to find out the truth.
 */
export const HLS_VERDICT_KEY = "lancast:hls-playable-2";

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

/*
 * Did the server actually hand over a playlist?
 *
 * `MEDIA_ERR_SRC_NOT_SUPPORTED` was treated as reliable evidence about the
 * engine. It is not, and the gap is what broke this: the element raises exactly
 * that code when it is handed something that is not media at all — including
 * the `503 {"error":…}` this server returns when ffmpeg has not produced
 * index.m3u8 within thirty seconds. So a slow start on one film was recorded as
 * "this device cannot play HLS", permanently, and every later film took the
 * progressive path.
 *
 * That is the same species of mistake the module was written about: a claim
 * about an engine, inferred rather than watched.
 *
 * So before writing a verdict, ask the endpoint directly. A response that is
 * not a playlist means the server failed, which says nothing about the engine
 * and must not be remembered. A real playlist that the element still refused is
 * the evidence the verdict was always supposed to rest on.
 *
 * Returning null means "no verdict" — deliberately distinct from "refused", so
 * a caller cannot fall into writing one by accident.
 */
export async function playlistWasServed(
  url: string,
  fetchImpl: typeof fetch = fetch,
): Promise<boolean | null> {
  try {
    const res = await fetchImpl(url, { headers: { Accept: HLS_MIME } });
    if (!res.ok) return false;
    const type = res.headers.get("content-type") ?? "";
    // A body that is not a playlist is a server saying something else — an
    // error page, a login redirect — and is not evidence about the engine.
    return type.includes("mpegurl");
  } catch {
    // The request could not be made at all. That is this moment, not this
    // device.
    return null;
  }
}
