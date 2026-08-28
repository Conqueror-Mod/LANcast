import { useDevice } from "./device";

/*
 * How a live channel is fed to the element.
 *
 * `progressive` is what has always happened: one endless fMP4 response into
 * `video.src`. It works, and everything in `preroll.ts` and `liveEdge.ts`
 * exists because it gives the client no control surface — a bare element cannot
 * say how much media it holds, cannot tell being starved from being stuck, and
 * cannot be seeked on a response with no ranges.
 *
 * `mse` feeds the same channel through hls.js into a `MediaSource`, which
 * answers all three directly. ADR 0013's amendment is the argument for it and
 * the record of what it cost to decide.
 *
 * Both exist at once on purpose, and the default stays `progressive`. This is
 * the transition setting from step 4 of that amendment: the new path has to be
 * livable-with before it is anybody's default, and the old one has to be one
 * toggle away while that is found out. Step 6 flips the default and deletes the
 * compensation code — deletion being the acceptance test — and this setting
 * goes with it.
 *
 * Per device rather than per account, following `spoilers` and `bigscreen`:
 * there is no per-user preference store on the server, and inventing one for a
 * transition toggle would be a schema decision made by a checkbox.
 */

export const LIVE_TRANSPORT_KEY = "lancast:live-transport";

export type LiveTransport = "progressive" | "mse";

export const LIVE_TRANSPORT_DEFAULT: LiveTransport = "progressive";

export function useLiveTransport(): [
  LiveTransport,
  (t: LiveTransport) => void,
] {
  return useDevice<LiveTransport>(LIVE_TRANSPORT_KEY, LIVE_TRANSPORT_DEFAULT);
}

/*
 * Whether MSE can be used at all here.
 *
 * Three separate questions, and answering them together is what stops a
 * device that cannot do this from being handed a black rectangle:
 *
 * - `MediaSource` may not exist. It is absent in older WebViews and on some
 *   television browsers, which is precisely the class of device this project
 *   expects to meet.
 * - It may exist and refuse the codecs. fMP4 carrying H.264 and AAC is the
 *   overwhelmingly common channel and the one worth asking about.
 * - Native HLS may make the whole question moot. Safari plays a playlist URL
 *   directly and must not be handed an MSE pipeline it does not need — that is
 *   step 5 of the amendment, and this reports the fact so the caller can act on
 *   it rather than deciding here.
 *
 * Pure, and takes its inputs, so it can be tested without a browser that has
 * any of this — jsdom has no MediaSource at all.
 */
export type MediaCapability = {
  hasMediaSource: boolean;
  isTypeSupported: (type: string) => boolean;
  canPlayType: (type: string) => string;
};

export const FMP4_H264_AAC = 'video/mp4; codecs="avc1.42E01E,mp4a.40.2"';
export const HLS_MIME = "application/vnd.apple.mpegurl";

export type LivePath = "mse" | "native-hls" | "progressive";

/**
 * Which path a channel should take, given the setting and what the device can
 * do.
 *
 * The setting can only ever ask for MSE; it cannot force it. A device without
 * `MediaSource` falls back to progressive rather than failing, because the
 * progressive path genuinely works and a setting is not worth a dead channel.
 */
export function livePath(
  setting: LiveTransport,
  cap: MediaCapability,
): LivePath {
  if (setting !== "mse") return "progressive";
  // Native HLS first: on a browser that has it, MSE is a pipeline built to
  // reach somewhere the browser already is.
  if (cap.canPlayType(HLS_MIME) !== "") return "native-hls";
  if (!cap.hasMediaSource) return "progressive";
  if (!cap.isTypeSupported(FMP4_H264_AAC)) return "progressive";
  return "mse";
}

/** What this browser can do, read from the real globals. */
export function mediaCapability(): MediaCapability {
  const MS = (globalThis as { MediaSource?: typeof MediaSource }).MediaSource;
  return {
    hasMediaSource: typeof MS !== "undefined",
    isTypeSupported: (t) => (MS ? MS.isTypeSupported(t) : false),
    canPlayType: (t) => {
      try {
        return document.createElement("video").canPlayType(t);
      } catch {
        return "";
      }
    },
  };
}
