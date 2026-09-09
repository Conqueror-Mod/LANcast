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
 * the better path on that device for ever.
 *
 * Bumping the key is how those devices get a second opinion. A verdict written
 * under the old rules is not worth migrating — it was reached by a test that
 * could not tell those failures apart — so it is left behind rather than read.
 */
export const HLS_VERDICT_KEY = "lancast:hls-playable-3";

/**
 * What this device has been observed to do, and how sure we are.
 *
 * `refusals` is why this is a record rather than a string: one refusal is an
 * incident and three is a property of the machine.
 */
export interface HLSRecord {
  verdict: HLSVerdict;
  /** Consecutive refusals. Reset by any success. */
  refusals: number;
  /** When it was last written, epoch ms. */
  at: number;
}

/*
 * A refusal is provisional until it repeats, and even then it does not last for
 * ever.
 *
 * Written once and read for ever was too strong, and it cost exactly what that
 * implies. On a real machine the element raised MEDIA_ERR_SRC_NOT_SUPPORTED
 * once, at 451ms, on a playlist that was **fine** — proven afterwards by
 * serving that same playlist, byte for byte, to the same engine, which played
 * it to readyState 4. The server had logged no error and the playlist fetch
 * that followed succeeded, so every check available said "the engine refused a
 * good playlist" and the device was pinned to the progressive path permanently
 * — the very path segments exist to replace.
 *
 * The lesson is not that the check was wrong. It is that **a single event is
 * not a property**. An engine that genuinely cannot read a playlist refuses
 * every one it is handed; a blip refuses once. Counting distinguishes them at a
 * cost of two extra failed loads, once, in the life of a device.
 *
 * SETTLED_AFTER is 3 rather than 2 because two consecutive failures are still
 * plausibly one bad minute — a server restarting mid-load will do it.
 */
const SETTLED_AFTER = 3;

/*
 * And it expires, because the answer can change under us.
 *
 * A WebView2 runtime updates, a codec extension is installed, a television
 * browser is replaced. Thirty days is long enough that a device which truly
 * cannot play a playlist pays the retry about once a month, and short enough
 * that somebody who fixed their machine is not still being told no next year.
 */
const SETTLED_FOR_MS = 30 * 24 * 60 * 60 * 1000;

export function hlsRecord(now = Date.now()): HLSRecord {
  const r = readDevice<HLSRecord | null>(HLS_VERDICT_KEY, null);
  if (!r || typeof r.verdict !== "string") {
    return { verdict: "unknown", refusals: 0, at: 0 };
  }
  // A settled refusal that has aged out becomes a question again.
  if (r.verdict === "refused" && r.refusals >= SETTLED_AFTER) {
    if (now - r.at > SETTLED_FOR_MS) {
      return { verdict: "unknown", refusals: 0, at: 0 };
    }
  }
  return r;
}

export function hlsVerdict(now = Date.now()): HLSVerdict {
  return hlsRecord(now).verdict;
}

/**
 * rememberHLS records what happened when this device was handed a playlist.
 *
 * A success settles it outright and clears any refusals behind it: frames from
 * a playlist are proof, where a failure to load is only evidence.
 */
export function rememberHLS(
  v: Exclude<HLSVerdict, "unknown">,
  now = Date.now(),
): void {
  if (v === "playable") {
    writeDevice<HLSRecord>(HLS_VERDICT_KEY, {
      verdict: "playable",
      refusals: 0,
      at: now,
    });
    return;
  }
  const prev = hlsRecord(now);
  writeDevice<HLSRecord>(HLS_VERDICT_KEY, {
    verdict: "refused",
    refusals: prev.verdict === "refused" ? prev.refusals + 1 : 1,
    at: now,
  });
}

/**
 * forgetHLS throws the verdict away, so the next film asks again.
 *
 * There was no way to do this, and that was the sharper half of the fault: the
 * only lever was bumping the storage key and shipping a release, which is a
 * migration wearing a constant's clothes. A remembered capability failure needs
 * a way back, the same way the codec denials do.
 */
export function forgetHLS(): void {
  writeDevice<HLSRecord>(HLS_VERDICT_KEY, {
    verdict: "unknown",
    refusals: 0,
    at: 0,
  });
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
  record: HLSRecord = hlsRecord(),
): boolean {
  if (record.verdict === "playable") return true;
  /*
   * A refusal only stops us once it has repeated. Below that it is an incident,
   * and the cost of asking again is one reload against the cost of never using
   * the better path again.
   */
  if (record.verdict === "refused" && record.refusals >= SETTLED_AFTER) {
    return false;
  }
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
