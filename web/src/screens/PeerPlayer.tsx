import { useEffect, useState } from "react";
import { useParams, Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { apiGet, apiSend } from "@/api/client";
import type { SubtitleTrack } from "@/api/types";
import { filePath, isUnsupportedSource } from "@/playback/fileTransport";
import { mediaCapability, HLS_MIME } from "@/lib/liveTransport";
import {
  peerItemURL,
  peerPlaybackURL,
  peerSourceURL,
  peerSubtitlesURL,
  peerSubtitleURL,
  peerWatchingURL,
} from "@/playback/peerSource";
import { PeerControls } from "@/components/PeerControls";
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
   * Asked for, not carried.
   *
   * The title used to ride in router state from the tile that was pressed,
   * which was lost the moment somebody reloaded or opened the address
   * directly. It is now asked of the server that owns the item — and it had to
   * be, because the same answer carries **how long the film is**, without which
   * a converted stream has a scrubber with no scale.
   */
  const info = useQuery({
    queryKey: ["peer-item", fingerprint, itemID],
    enabled: fingerprint !== "" && itemID > 0,
    retry: false,
    queryFn: ({ signal }) =>
      apiGet<{ title?: string; duration_ms?: number }>(
        peerItemURL(fingerprint, itemID),
        signal,
      ),
  });
  const title = info.data?.title;
  /*
   * The film's own length, from whoever probed the file.
   *
   * It cannot come from the media element on a converted stream: each session
   * starts at zero and reports only what it has produced so far, which is a few
   * seconds. Direct play is the opposite — those are the file's real bytes, so
   * the element is authoritative and this is the fallback.
   */
  const probed = (info.data?.duration_ms ?? 0) / 1000;

  /*
   * A callback ref, not useRef, and that is the whole of a shipped bug.
   *
   * The element does not exist on mount: this screen returns a note while the
   * far server's decision is still in flight, so there is no <video> to hang a
   * listener on. With `useRef` the effects ran once, found `current` null,
   * returned, and never ran again — their dependencies had not changed when the
   * element finally appeared. **No listener was ever attached**, so nothing
   * reported that a film was playing and presence stayed idle in both
   * directions.
   *
   * State holds the element instead, so it becomes a dependency: the effects
   * run when there is something to wire, which is the only moment they could
   * ever have worked.
   */
  const [video, setVideo] = useState<HTMLVideoElement | null>(null);
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
  /*
   * Where the current session starts within the film.
   *
   * Zero for direct play, always: the element owns that timeline. For a
   * converted stream this is the only record of where we are, because the
   * element's clock restarts from zero at every seek.
   */
  const [offset, setOffset] = useState(0);
  const [elapsed, setElapsed] = useState(0);
  const [paused, setPaused] = useState(true);

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
   * The beat that makes this visible as watching (ADR 0045 §10).
   *
   * Locally, presence is a side effect of the progress write. A peer item
   * writes no progress — ADR 0071 §4 leaves a friend's progress genuinely
   * undecided — so without this the People screen says *idle* while a film is
   * on screen, which is a false statement about the present and the one thing
   * ADR 0045 exists not to make.
   *
   * Five seconds, matching the local progress beat, against a twenty-second
   * expiry: three missed beats is a network hiccup, not somebody who stopped.
   *
   * Only while the picture is moving. Pausing stops the beat and presence
   * expires on its own, which is the same thing pausing does locally — and it
   * is why nothing needs to be sent on the way out. A beat that stops is
   * indistinguishable from a client that closed, which is correct: both mean
   * nobody is watching this now.
   */
  useEffect(() => {
    const el = video;
    if (!el || fingerprint === "" || itemID <= 0) return;

    let timer: ReturnType<typeof setInterval> | undefined;
    const beat = () => {
      // Errors are swallowed on purpose. A failed beat means presence expires,
      // which is the truthful outcome, and a viewer must never be told their
      // film is in trouble because a *disclosure* did not go through.
      void apiSend(peerWatchingURL(fingerprint, itemID), "PUT").catch(() => {});
    };
    const start = () => {
      if (timer !== undefined) return;
      beat();
      timer = setInterval(beat, 5000);
    };
    const stop = () => {
      if (timer === undefined) return;
      clearInterval(timer);
      timer = undefined;
    };

    el.addEventListener("playing", start);
    el.addEventListener("pause", stop);
    el.addEventListener("ended", stop);
    el.addEventListener("emptied", stop);
    if (!el.paused) start();

    return () => {
      stop();
      el.removeEventListener("playing", start);
      el.removeEventListener("pause", stop);
      el.removeEventListener("ended", stop);
      el.removeEventListener("emptied", stop);
    };
  }, [fingerprint, itemID, video]);

  /*
   * The clock, and it is not the element's on a converted stream.
   *
   * Each session starts at zero, so what the film is at is the offset this
   * session began at plus however far into it we are. Direct play keeps an
   * offset of zero and this is the element's own time, unchanged.
   */
  useEffect(() => {
    const el = video;
    if (!el) return;
    const tick = () => setElapsed(el.currentTime);
    const onPlay = () => setPaused(false);
    const onPause = () => setPaused(true);
    el.addEventListener("timeupdate", tick);
    el.addEventListener("play", onPlay);
    el.addEventListener("pause", onPause);
    return () => {
      el.removeEventListener("timeupdate", tick);
      el.removeEventListener("play", onPlay);
      el.removeEventListener("pause", onPause);
    };
  }, [video]);

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
    const el = video;
    if (!el) return;
    const onError = () => {
      if (hlsUsable && isUnsupportedSource(el.error)) setHLSUsable(false);
    };
    el.addEventListener("error", onError);
    return () => el.removeEventListener("error", onError);
  }, [hlsUsable, video]);

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
  const converting = path !== "direct";
  const src = peerSourceURL(fingerprint, itemID, path, offset);
  const tracks = (subtitles.data?.subtitles ?? []).filter((t) => t.available);

  // What the film is at, and how long it is. See the offset and probed
  // comments above for why neither is simply the element's own value.
  const at = converting ? offset + elapsed : elapsed;
  const total = converting ? probed : video?.duration || probed;

  /*
   * Seeking a converted stream is not a seek.
   *
   * There is nothing to seek within: the bytes do not exist until the far
   * server makes them. So it asks them to start again somewhere else, and the
   * offset is how the screen goes on telling the truth about a clock that just
   * went back to zero.
   *
   * Direct play is left entirely alone — those are the file's own bytes and the
   * element does this better than we would.
   */
  const seek = (to: number) => {
    const el = video;
    if (!el) return;
    const target = Math.max(0, total > 0 ? Math.min(to, total) : to);
    if (!converting) {
      el.currentTime = target;
      return;
    }
    setOffset(target);
    setElapsed(0);
    el.src = peerSourceURL(fingerprint, itemID, path, target);
    el.load();
    resume(el);
  };

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

        Its own controls rather than the element's, because the element's
        would lie. Native controls draw the *element's* timeline, and on a
        converted stream that restarts at zero at every seek — so after moving
        to forty minutes the scrubber would read nought against a duration of a
        few seconds. That is how "it will not seek" was reported. PeerControls
        draws the film's clock: this session's offset plus the element's own.
      */}
      <video
        ref={setVideo}
        className="peer-player__video"
        src={src}
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

      <PeerControls
        paused={paused}
        at={at}
        total={total}
        converting={converting}
        onPlayPause={() => {
          const el = video;
          if (!el) return;
          if (el.paused) resume(el);
          else el.pause();
        }}
        onSeek={seek}
      />

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

/*
 * play() does not always return a promise.
 *
 * It returns one in every engine this app ships against, and **nothing** in
 * older ones — and in jsdom, which is how this was found: `play().catch(…)`
 * threw `Cannot read properties of undefined`, inside an event handler, where
 * a throw is not the caller's to catch.
 *
 * Wrapping is two characters wider than asserting the promise exists, and it
 * turns a class of environment into a non-event rather than a crash.
 */
function resume(el: HTMLMediaElement) {
  void Promise.resolve(el.play()).catch(() => {});
}
