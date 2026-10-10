import { useEffect, useRef, useState } from "react";
import { useParams, useSearchParams, Link } from "react-router-dom";
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
  peerProgressURL,
  peerWatchingURL,
  asMember,
} from "@/playback/peerSource";
import {
  usePeerRoom,
  expectedPosition,
  followerShouldSeek,
  followerSeekTarget,
  followerShouldCorrect,
  nextLead,
  CONVERTING_DEFAULT_LEAD_MS,
} from "@/playback/together";
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
   * In a room on their server (federation Phase 5), arrived at by being let
   * in from the People page. Every playback URL then says so, because a film
   * that was not shared is playable only by being in the room, and their
   * server checks that by person. The host drives; this player follows.
   */
  const [params] = useSearchParams();
  const roomID = params.get("room");
  const member = roomID !== null && roomID !== "";
  const room = usePeerRoom(fingerprint, member ? roomID : null);
  const host = room.session?.members.find((m) => m.host)?.name;
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
    queryKey: ["peer-item", fingerprint, itemID, member],
    enabled: fingerprint !== "" && itemID > 0,
    retry: false,
    queryFn: ({ signal }) =>
      apiGet<{ title?: string; duration_ms?: number; position_ms?: number }>(
        asMember(peerItemURL(fingerprint, itemID), member),
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
  /*
   * Volume is the viewer's, not the film's.
   *
   * Shared with the household player through the same key, so a friend's film
   * starts as loud as your own did rather than at whatever the element
   * defaults to — which is full, and a jump in level nobody asked for.
   */
  const [volume, setVolume] = useState(() => {
    try {
      const raw = localStorage.getItem("lancast:volume");
      const saved = raw === null ? NaN : Number(raw);
      return saved >= 0 && saved <= 1 ? saved : 1;
    } catch {
      return 1;
    }
  });
  const [muted, setMuted] = useState(false);
  const [fullscreen, setFullscreen] = useState(false);

  // Applied on every new source too: a converted seek swaps `src`, and the
  // element goes back to its defaults when it loads one.
  useEffect(() => {
    const el = video;
    if (!el) return;
    const apply = () => {
      el.volume = volume;
      el.muted = muted;
    };
    apply();
    el.addEventListener("loadedmetadata", apply);
    return () => el.removeEventListener("loadedmetadata", apply);
  }, [video, volume, muted]);

  const changeVolume = (next: number) => {
    const v = Math.min(1, Math.max(0, next));
    setVolume(v);
    // Raising the level unmutes, which is what dragging the slider up means.
    if (v > 0) setMuted(false);
    try {
      localStorage.setItem("lancast:volume", String(v));
    } catch {
      // A private window may refuse storage; the level still applies now.
    }
  };

  // Unmuting from a level of zero would be silent anyway, so it restores one.
  const toggleMute = () => {
    if (muted) setMuted(false);
    else if (volume === 0) changeVolume(1);
    else setMuted(true);
  };

  /*
   * Fullscreen is the window's job in the desktop client, as in the household
   * player: WebView2 hands requestFullscreen to its host and changes nothing
   * itself, so the host's binding is asked when it exists. In a browser the
   * Fullscreen API is right.
   *
   * Either way the *screen* also has to become the whole viewport — the
   * window filling the monitor does nothing for a video laid out inside the
   * page beside the rail. That is the `--fullscreen` class.
   */
  const toggleFullscreen = () => {
    const bound = (
      window as { lancastToggleFullscreen?: () => Promise<boolean> }
    ).lancastToggleFullscreen;
    if (bound) {
      void bound().then((on) => setFullscreen(Boolean(on)));
      return;
    }
    if (document.fullscreenElement) {
      void document.exitFullscreen().catch(() => {});
      setFullscreen(false);
      return;
    }
    void document.documentElement.requestFullscreen().catch(() => {});
    setFullscreen(true);
  };
  const toggleFullscreenRef = useRef(toggleFullscreen);
  toggleFullscreenRef.current = toggleFullscreen;

  // The browser's own exits (Escape, F11) happen without asking us.
  useEffect(() => {
    const sync = () => {
      if (!(window as { lancastToggleFullscreen?: unknown })
        .lancastToggleFullscreen)
        setFullscreen(Boolean(document.fullscreenElement));
    };
    document.addEventListener("fullscreenchange", sync);
    return () => document.removeEventListener("fullscreenchange", sync);
  }, []);

  // Escape leaves, and leaving the screen never strands the window full.
  useEffect(() => {
    if (!fullscreen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") toggleFullscreenRef.current();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [fullscreen]);
  const fullscreenRef = useRef(fullscreen);
  fullscreenRef.current = fullscreen;
  useEffect(
    () => () => {
      if (fullscreenRef.current) toggleFullscreenRef.current();
    },
    [],
  );
  /*
   * Resumed once, and only once.
   *
   * The position arrives with the item information, after the first render, so
   * it has to be applied when it lands — and then never again, because every
   * later render would otherwise drag the film back to where it started. A ref
   * rather than state: nothing draws it, and a re-render to record that a
   * decision was already made is a render for nothing.
   */
  const resumed = useRef(false);
  /*
   * The latest `seek` and the latest position, reachable from the effects.
   *
   * Both are computed *after* the early returns below — they depend on the far
   * server's decision, which is still in flight on the first render — so an
   * effect declared up here cannot close over them. Refs hold the current value
   * without making every effect re-subscribe each time the clock ticks, which
   * for a fifteen-second timer would mean tearing it down four times a minute.
   */
  const seekRef = useRef<((to: number) => void) | null>(null);
  const atRef = useRef(0);

  const playback = useQuery({
    queryKey: ["peer-playback", fingerprint, itemID, member],
    enabled: fingerprint !== "" && itemID > 0,
    retry: false,
    queryFn: ({ signal }) =>
      apiGet<{ decision: { method: string; reason: string } }>(
        asMember(peerPlaybackURL(fingerprint, itemID), member),
        signal,
      ),
  });

  const subtitles = useQuery({
    queryKey: ["peer-subtitles", fingerprint, itemID, member],
    enabled: fingerprint !== "" && itemID > 0,
    retry: false,
    queryFn: ({ signal }) =>
      apiGet<{ subtitles: SubtitleTrack[] }>(
        asMember(peerSubtitlesURL(fingerprint, itemID), member),
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
      void apiSend(asMember(peerWatchingURL(fingerprint, itemID), member), "PUT").catch(() => {});
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
  }, [fingerprint, itemID, member, video]);

  /*
   * Picking up where this household left off (ADR 0071 §4).
   *
   * Their server holds the film; **ours** holds where we are in it, because §4
   * puts a friend's progress on the friend's own server — a row on the host
   * keyed to a remote principal is an account by another name. So the host
   * neither writes nor knows this.
   *
   * Applied through `seek`, which already knows the difference between the two
   * delivery paths: a converted stream cannot be moved by setting a clock, it
   * has to be asked for again from the new position.
   */
  useEffect(() => {
    const at = (info.data?.position_ms ?? 0) / 1000;
    // In a room the host's position is where the film is, not ours.
    if (member) return;
    if (!video || resumed.current || at <= 0) return;
    resumed.current = true;
    seekRef.current?.(at);
  }, [video, info.data?.position_ms, member]);

  /*
   * Writing it down, which is the one record a peer's film produces.
   *
   * Every fifteen seconds rather than the beat's five: presence is a claim
   * about *now* and is wrong the moment it is late, while a resume position is
   * allowed to be a few seconds behind — and this one crosses no network
   * beyond our own server, but it is a *write*, and a write every five seconds
   * for a two-hour film is 1,440 of them.
   *
   * On pause as well, because pausing is the most likely moment somebody walks
   * away, and the interval would otherwise lose up to fifteen seconds of it.
   */
  useEffect(() => {
    const el = video;
    if (!el || fingerprint === "" || itemID <= 0) return;

    const save = () => {
      const at = atRef.current;
      if (at <= 0) return;
      void apiSend(peerProgressURL(fingerprint, itemID), "PUT", {
        position_ms: Math.floor(at * 1000),
      }).catch(() => {});
    };
    const timer = setInterval(() => {
      if (!el.paused) save();
    }, 15_000);
    el.addEventListener("pause", save);
    return () => {
      clearInterval(timer);
      el.removeEventListener("pause", save);
      // Not saved on the way out. Leaving the screen is indistinguishable from
      // the tab closing, and the interval and the pause between them have
      // already recorded anything worth keeping.
    };
  }, [video, fingerprint, itemID]);

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

  /*
   * Following the host, on every answer from the room.
   *
   * Where the film should be is the room's position plus how long ago the
   * host's server said so (age_ms) plus how long ago this device heard it:
   * no clock on this machine is compared with any clock on theirs. Whether
   * that is far enough out to seek is followerShouldSeek's, which waits out a
   * converted stream's restart instead of seeking again into it.
   */
  const lastSeek = useRef(0);
  const convertingRef = useRef(false);
  /*
   * Whether the stream has played since the last seek, and the lead that
   * seek used. A converted stream is not moved again until it has started;
   * when it does, where it landed against the host teaches the next lead
   * (nextLead), and a gap worth closing is closed with at most two more
   * seeks (followerShouldCorrect). See together.ts for both.
   */
  const startedSinceSeek = useRef(true);
  const leadMS = useRef(CONVERTING_DEFAULT_LEAD_MS);
  const leadUsed = useRef(0);
  const playingSince = useRef(0);
  const corrections = useRef(0);
  const roomRef = useRef(room);
  roomRef.current = room;
  useEffect(() => {
    const el = video;
    if (!el) return;
    const onPlaying = () => {
      if (startedSinceSeek.current) return;
      startedSinceSeek.current = true;
      playingSince.current = Date.now();
      const r = roomRef.current;
      if (convertingRef.current && r.session && lastSeek.current > 0) {
        const gap = expectedPosition(r.session, r.receivedAt, Date.now()) - atRef.current * 1000;
        leadMS.current = nextLead(leadUsed.current, gap);
      }
    };
    el.addEventListener("playing", onPlaying);
    return () => el.removeEventListener("playing", onPlaying);
  }, [video]);
  useEffect(() => {
    const el = video;
    const sess = room.session;
    if (!member || !el || !sess) return;
    const expected = expectedPosition(sess, room.receivedAt, Date.now());
    const now = Date.now();
    const far = followerShouldSeek(
      atRef.current * 1000,
      expected,
      convertingRef.current,
      now - lastSeek.current,
      startedSinceSeek.current,
    );
    const close =
      !far &&
      startedSinceSeek.current &&
      followerShouldCorrect(
        atRef.current * 1000,
        expected,
        convertingRef.current,
        now - playingSince.current,
        corrections.current,
      );
    if (far || close) {
      if (close) corrections.current++;
      lastSeek.current = now;
      startedSinceSeek.current = false;
      leadUsed.current = convertingRef.current ? leadMS.current : 0;
      seekRef.current?.(followerSeekTarget(expected, convertingRef.current, leadUsed.current) / 1000);
    }
    if (sess.paused && !el.paused) el.pause();
    if (!sess.paused && el.paused) resume(el);
  }, [member, video, room.session, room.receivedAt]);

  // The room is over: stop, rather than go on playing a film that was only
  // ever ours to watch with them.
  useEffect(() => {
    if (member && room.ended && video && !video.paused) video.pause();
  }, [member, room.ended, video]);

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
  const src = asMember(peerSourceURL(fingerprint, itemID, path, offset), member);
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
    el.src = asMember(peerSourceURL(fingerprint, itemID, path, target), member);
    el.load();
    resume(el);
  };

  // Kept current for the effects above, which run before either exists.
  seekRef.current = seek;
  atRef.current = at;
  convertingRef.current = converting;

  return (
    <div
      className={`peer-player${fullscreen ? " peer-player--fullscreen" : ""}`}
    >
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
            src={asMember(peerSubtitleURL(fingerprint, itemID, t.key), member)}
          />
        ))}
      </video>

      <PeerControls
        paused={paused}
        at={at}
        total={total}
        converting={converting}
        onPlayPause={() => {
          // In a room the host drives (ADR 0046 §9): a follower pausing would
          // be corrected on the next poll, which reads as a broken button.
          if (member) return;
          const el = video;
          if (!el) return;
          if (el.paused) resume(el);
          else el.pause();
        }}
        onSeek={member ? () => {} : seek}
        following={member}
        volume={volume}
        muted={muted}
        onVolume={changeVolume}
        onToggleMute={toggleMute}
        fullscreen={fullscreen}
        onToggleFullscreen={toggleFullscreen}
      />

      {/*
        Two facts, and the second is the interesting one.

        ADR 0071 §4 puts a friend's progress on the *friend's* server, because a
        row on the host keyed to a remote principal is an account by another
        name — it outlives the evening, it has to be listed and deleted, and
        unpairing would no longer be complete.

        So where somebody is in a film is a thing this household knows and the
        host does not, and saying so is worth a line: it is the difference
        between a position being private and a person assuming it is.
      */}
      {member ? (
        <p className="peer-player__note" role="status">
          {room.ended
            ? "This session has ended."
            : host
              ? `Watching with ${host}. They control playback.`
              : "Joining the session…"}
        </p>
      ) : (
        <p className="peer-player__note">
          Playing from their machine. Where you are in it is kept here, not there
          — they cannot see it, and unpairing forgets it.
        </p>
      )}
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
