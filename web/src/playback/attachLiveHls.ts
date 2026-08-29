/*
 * The only place in this client that touches hls.js.
 *
 * Kept to one file so the dependency's blast radius is a filename rather than
 * "the live screen". If it is ever removed, this file goes and one call site
 * changes — which is the property ADR 0013's amendment asked for when it said
 * removal should be a deletion rather than a rewrite.
 *
 * The import is **dynamic**, and that is not a style preference. The vendored
 * bundle is 618 KB against a whole client bundle of about 460 KB: imported at
 * the top level it would more than double what every viewer downloads, to
 * serve a setting that is off by default and a screen most sessions never open.
 * A dynamic import puts it in its own chunk, fetched the first time somebody
 * actually plays a channel with the setting on.
 */

import type Hls from "hls.js";

/**
 * Said when the server does not have the endpoint this path needs.
 *
 * Exported so the screen can recognise it rather than pattern-matching on
 * prose, and so a test can assert the distinction without asserting the
 * wording.
 */
export const OLD_SERVER = "server-too-old";

/** What a caller needs to take back: stop it, and let go of the element. */
export type LiveAttachment = {
  destroy: () => void;
};

/*
 * Config we set, and why each line is here.
 *
 * Everything not named is left at the library's default deliberately — the
 * argument for adopting this was that it already knows how to do the things
 * `preroll.ts` guesses at, and overriding those on day one would be
 * reintroducing the guesses through a different door.
 */
export function liveHlsConfig() {
  return {
    /*
     * Off, explicitly, and this is the one that matters.
     *
     * CMCD reports playback telemetry to a CDN. It defaults to undefined and
     * we set it anyway: the no-phone-home principle in README.md is one of the
     * four this project does not negotiate, and a default is something an
     * upgrade can change quietly. See web/vendor/hls.js/README.md.
     */
    cmcd: undefined,
    // DRM off for the same reason: a licence request is an outbound call to
    // somebody else's server, and no channel here needs one.
    emeEnabled: false,
    /*
     * Start at the live edge rather than at the beginning of the playlist.
     *
     * The server emits an EVENT playlist, which keeps every segment produced
     * since the session started (see internal/transcode/args.go). Without this,
     * a viewer joining a channel that has been running for ten minutes starts
     * ten minutes behind and stays there — which is the drift fault this whole
     * line of work exists to close, arriving from the opposite direction.
     */
    liveSyncDurationCount: 3,
  };
}

/**
 * Attach hls.js to an element for one channel.
 *
 * Resolves once the library is loaded and attached, not once media is playing:
 * whether to play is the caller's decision, and on a live channel it is
 * entangled with autoplay policy the caller already handles.
 *
 * `onReady` is how the caller learns *when* that decision can be acted on, and
 * it exists because the obvious moment is the wrong one. `attachMedia` returns
 * before the MediaSource reaches the element — the object URL is set in a later
 * task — so a `play()` on the line after it runs against an element with no
 * source at all and rejects outright. Resolving this promise is therefore not a
 * signal that anything is playable; `MANIFEST_PARSED` is.
 */
export async function attachLiveHls(
  el: HTMLVideoElement,
  channelID: number,
  onError?: (fatal: boolean, detail: string) => void,
  onReady?: () => void,
): Promise<LiveAttachment> {
  const mod = await import("hls.js");
  const HlsCtor = mod.default;

  const hls: Hls = new HlsCtor(liveHlsConfig());

  hls.on(HlsCtor.Events.ERROR, (_e, data) => {
    /*
     * A 404 on the playlist means the *server* is too old, not that the
     * channel is off the air.
     *
     * The endpoint this path needs shipped after v0.8.20, so a client updated
     * ahead of its server asks for something that is not there. Reporting that
     * as "the channel stopped" sends somebody to their provider to debug a
     * channel that is fine — the same class of misdirection as the activity
     * panel calling every session a transcode, which cost this project an hour
     * on the evidence of a badge.
     */
    const status = (data.response as { code?: number } | undefined)?.code;
    if (data.fatal === true && status === 404) {
      onError?.(true, OLD_SERVER);
      return;
    }
    /*
     * Non-fatal errors are ordinary and are not reported upward.
     *
     * A live stream produces them steadily — a segment that 404s at the edge, a
     * gap skipped, a level switched. Surfacing those would recreate the exact
     * fault this transport is meant to fix: `waiting` fired about once a second
     * on a healthy channel and was treated as a drought, which held the player
     * 28% of the time. Only a fatal error means the channel has stopped.
     */
    onError?.(data.fatal === true, `${data.type}: ${data.details}`);
  });

  /*
   * The moment there is something to play.
   *
   * MANIFEST_PARSED means the playlist is loaded and a level is chosen, which
   * is the earliest point at which `play()` means anything. Nothing earlier
   * does: between `attachMedia` and this, the element has no source, and a
   * `play()` there rejects and is gone.
   */
  hls.on(HlsCtor.Events.MANIFEST_PARSED, () => onReady?.());

  hls.loadSource(`/api/channels/${channelID}/hls/index.m3u8`);
  hls.attachMedia(el);

  return {
    destroy: () => {
      // detachMedia before destroy: destroy alone can leave the element with a
      // src object it no longer owns, and the next channel then attaches to a
      // element that is still holding the last one's MediaSource.
      try {
        hls.detachMedia();
      } catch {
        // Already gone. Nothing to do, and throwing here would mask whatever
        // the caller was actually cleaning up.
      }
      hls.destroy();
    },
  };
}
