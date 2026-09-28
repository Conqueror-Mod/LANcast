import { useEffect, useRef, useState } from "react";
import { useParams, useLocation, Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { apiGet } from "@/api/client";
import type { SubtitleTrack } from "@/api/types";
import { filePath, isUnsupportedSource } from "@/playback/fileTransport";
import { mediaCapability, HLS_MIME } from "@/lib/liveTransport";
import {
  peerPlaybackURL,
  peerSourceURL,
  peerSubtitlesURL,
  peerSubtitleURL,
} from "@/playback/peerSource";
import "./PeerPlayer.css";

/*
 * Watching something on somebody else's server
 * ([ADR 0071](../../../docs/adr/0071-a-shared-library-is-a-standing-grant.md)
 * §5).
 *
 * # Its own player, for the same reason PeerLibrary is its own screen
 *
 * The household's player carries a queue, resume, progress writes, watch
 * state, "are you still watching" and the up-next lane — all keyed on a local
 * item id, and **none of which a peer item may touch**. A peer item must never
 * reach Continue Watching, Recently Added, a count or the watch history, and
 * §5 is explicit that the way to guarantee that is for the two never to share
 * a code path that could be taught to concatenate.
 *
 * Threading a peer through a thousand lines of provider would have put that
 * guarantee in the hands of every future edit to a file about something else.
 *
 * # Nothing is recorded, anywhere
 *
 * A peer item starts at the beginning, every time, and writes nothing here or
 * there. Where a friend's progress should live is ADR 0071 §4 and is genuinely
 * undecided; storing it locally "for now" would answer that question by
 * accident, in the place §5 says nothing about a peer belongs.
 *
 * So there is no resume, and that is a decision rather than an omission.
 */
export function PeerPlayer() {
  const { fingerprint = "", item = "" } = useParams();
  const itemID = Number(item);
  /*
   * The title comes with the navigation when there is one.
   *
   * The alternative is asking the far server for one item, which it has no
   * route for — it browses a library at a time — so this would mean fetching
   * a whole page and searching it for a name. A heading is not worth another
   * household's disk spinning, and arriving without one is survivable: the
   * screen simply says it is playing something of theirs.
   */
  const title = (useLocation().state as { title?: string } | null)?.title;

  const video = useRef<HTMLVideoElement>(null);
  /*
   * Whether this engine can be given a playlist.
   *
   * Read once from the real element rather than assumed. The desktop client is
   * Chromium and plays HLS natively; a browser tab may not, and the honest
   * fallback is the progressive transcode, which genuinely works.
   */
  const [hlsUsable, setHLSUsable] = useState(() =>
    mediaCapability().canPlayType(HLS_MIME) !== "",
  );

  const playback = useQuery({
    queryKey: ["peer-playback", fingerprint, itemID],
    enabled: fingerprint !== "" && itemID > 0,
    retry: false,
    queryFn: ({ signal }) =>
      apiGet<{ decision: { method: string; reason: string } }>(
        peerPlaybackURL(fingerprint, itemID),
        signal,
      ),
  });

  const subtitles = useQuery({
    queryKey: ["peer-subtitles", fingerprint, itemID],
    enabled: fingerprint !== "" && itemID > 0,
    retry: false,
    queryFn: ({ signal }) =>
      apiGet<{ subtitles: SubtitleTrack[] }>(
        peerSubtitlesURL(fingerprint, itemID),
        signal,
      ),
  });

  /*
   * One retirement of the playlist route, on the element's own evidence.
   *
   * `MEDIA_ERR_SRC_NOT_SUPPORTED` is what an engine without HLS does with a
   * playlist. It is deliberately the *only* error treated this way: a decode
   * error or a network error is a statement about this file or this moment,
   * and treating either as a verdict on the device would retire the better
   * path for ever over one slow transcode.
   */
  useEffect(() => {
    const el = video.current;
    if (!el) return;
    const onError = () => {
      if (hlsUsable && isUnsupportedSource(el.error)) setHLSUsable(false);
    };
    el.addEventListener("error", onError);
    return () => el.removeEventListener("error", onError);
  }, [hlsUsable]);

  if (!fingerprint || itemID <= 0) {
    return <PeerPlayerNote>That is not something on another server.</PeerPlayerNote>;
  }

  if (playback.isLoading) {
    return <PeerPlayerNote>Asking their server…</PeerPlayerNote>;
  }

  /*
   * Said as a fact about them, not as a fault here.
   *
   * Their machine being off is the ordinary state of somebody else's computer,
   * and a screen that presented it as an error in this house would send
   * somebody looking through their own settings for it.
   */
  if (playback.isError || !playback.data) {
    return (
      <PeerPlayerNote title={title}>
        That server is not answering, so this cannot be played right now. It is
        only here while their machine is on.
      </PeerPlayerNote>
    );
  }

  const path = filePath(playback.data.decision.method, hlsUsable);
  const src = peerSourceURL(fingerprint, itemID, path);
  const tracks = (subtitles.data?.subtitles ?? []).filter((t) => t.available);

  return (
    <div className="peer-player">
      <header className="peer-player__head">
        <Link to="/people" className="peer-player__back">
          On another server
        </Link>
        <h1 className="peer-player__title">
          {title ?? "Something on their server"}
        </h1>
      </header>

      {/*
        No third-party player library, on the file path (ADR 0013, amended by
        ADR 0050). hls.js is vendored for live channels only and is not reached
        from here — a playlist plays natively or the progressive transcode is
        taken instead.

        `controls` rather than this project's own chrome, and only for now: the
        chrome is built around the provider's clock, its resume and its queue,
        none of which exist here. Borrowing it would mean drawing a scrubber
        over state nothing is keeping.
      */}
      <video
        ref={video}
        className="peer-player__video"
        src={src}
        controls
        autoPlay
        // Never `metadata` — a peer item has no poster to hold the frame, and
        // preloading a film on somebody else's connection to draw one is their
        // bandwidth spent on our decoration.
        preload="none"
        crossOrigin="anonymous"
      >
        {tracks.map((t) => (
          <track
            key={t.key}
            kind="subtitles"
            label={t.label}
            srcLang={t.language}
            default={t.default}
            src={peerSubtitleURL(fingerprint, itemID, t.key)}
          />
        ))}
      </video>

      <p className="peer-player__note">
        Playing from their machine. Nothing about this is recorded here or
        there, so it starts from the beginning each time.
      </p>
    </div>
  );
}

function PeerPlayerNote({
  children,
  title,
}: {
  children: React.ReactNode;
  title?: string;
}) {
  return (
    <div className="peer-player">
      <header className="peer-player__head">
        <Link to="/people" className="peer-player__back">
          On another server
        </Link>
        {title && <h1 className="peer-player__title">{title}</h1>}
      </header>
      <p className="peer-player__note">{children}</p>
    </div>
  );
}
