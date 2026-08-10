import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useItem, useSubtitles } from "@/api/hooks";
import { apiGet, apiSend, artworkURL } from "@/api/client";
import type { Item, SubtitleTrack, MediaStream } from "@/api/types";
import { withCapabilities, capabilities, deny, resetCapabilities } from "./capabilities";

// Playback lives above the router.
//
// It used to live in the Player screen, which meant the media element was a
// child of a route: leaving the route unmounted the element and the sound
// stopped. That is right for a film — you watch it and you are done — and wrong
// for a record, which you put on and then go and browse. Hoisting it here is
// what lets a mini-player survive navigation, and it is the one structural
// change the music work needed (docs/music-client-plan.md).
//
// Everything the engine does is unchanged and deliberately so: the same source
// selection, resume, progress writes, transcode seeking, queue advance and
// volume memory, moved rather than rewritten. The screen above it became chrome.

interface Decision {
  method: "direct" | "remux" | "transcode";
  reason: string;
}

// A direct/remux source is a real file with Range support, so the browser seeks
// natively. A transcode has no length and cannot be range-served: seeking means
// restarting ffmpeg at a new offset, and the displayed time is that offset plus
// the element's own clock.
function sourceURL(
  id: number,
  method: Decision["method"],
  offset: number,
  audio?: number | null,
): string {
  // ?audio= names the absolute stream index (docs/api.md). It participates in
  // the delivery decision rather than only in stream selection, because a file
  // that direct-plays with its default track may need converting to deliver a
  // different one — a second audio track is often the one codec the browser
  // cannot decode.
  const a = audio != null ? `audio=${audio}` : "";
  if (method === "direct") {
    return a ? `/api/stream/${id}?${a}` : `/api/stream/${id}`;
  }
  const parts = [offset > 0 ? `t=${Math.floor(offset)}` : "", a].filter(Boolean);
  const t = parts.length > 0 ? `?${parts.join("&")}` : "";
  // Carries the same capabilities the decision was made with. The transcode
  // endpoint decides again from its own parameters, so a request claiming less
  // than the one before it gets a different answer — the file the server just
  // called direct-playable is re-encoded, or refused with a 409.
  return withCapabilities(`/api/stream/${id}/transcode${t}`);
}

// Repeat cycles off -> all -> one, which is the order every music player uses:
// each press is "more repetition than before".
export type RepeatMode = "off" | "all" | "one";

// Surface is where the media element is drawn. "full" is the player screen,
// "mini" the docked corner, "idle" nothing playing.
export type Surface = "full" | "mini" | "idle";

interface PlaybackState {
  itemID: number;
  item: Item | undefined;
  isAudio: boolean;
  cover: string | undefined;
  surface: Surface;

  playing: boolean;
  loading: boolean;
  displayTime: number;
  totalDuration: number;
  muted: boolean;
  volume: number;
  note: string;

  subtitles: SubtitleTrack[];
  activeSub: SubtitleTrack | null;
  subKey: string | null;

  /** Alternate audio tracks the file carries, from the probe. */
  audioTracks: MediaStream[];
  /** The chosen track's absolute stream index, null for the file's default. */
  audioIndex: number | null;
  speed: number;
  shuffle: boolean;
  repeat: RepeatMode;
  /** Item ids queued behind this one, in play order. */
  queue: number[];
  /** Whether there is anything to move to in each direction. */
  hasNext: boolean;
  hasPrev: boolean;

  play: (id: number, queue: number[]) => void;
  stop: () => void;
  togglePlay: () => void;
  seekTo: (t: number) => void;
  seekBy: (d: number) => void;
  toggleMute: () => void;
  changeVolume: (v: number) => void;
  toggleFullscreen: () => void;
  cycleSub: (dir: 1 | -1) => void;
  selectSub: (key: string | null) => void;
  selectAudio: (index: number | null) => void;
  setSpeed: (rate: number) => void;
  toggleShuffle: () => void;
  cycleRepeat: () => void;
  playNext: () => void;
  playPrev: () => void;
  playFromQueue: (id: number) => void;

  videoRef: React.RefObject<HTMLVideoElement>;
  containerRef: React.RefObject<HTMLDivElement>;
  /** Claimed by the player screen while it is mounted. */
  claimFullSurface: (claimed: boolean) => void;
}

const Ctx = createContext<PlaybackState | null>(null);

export function usePlayback(): PlaybackState {
  const v = useContext(Ctx);
  if (!v) throw new Error("usePlayback outside PlaybackProvider");
  return v;
}

/**
 * useFullSurface claims the full-screen surface for as long as the calling
 * screen is mounted, and hands it back on unmount — which is what turns the
 * player into the docked mini-player when you navigate away.
 */
export function useFullSurface() {
  const { claimFullSurface } = usePlayback();
  useEffect(() => {
    claimFullSurface(true);
    return () => claimFullSurface(false);
  }, [claimFullSurface]);
}

export function PlaybackProvider({ children }: { children: ReactNode }) {
  const [itemID, setItemID] = useState(0);
  const [queue, setQueue] = useState<number[]>([]);
  const [fullClaimed, setFullClaimed] = useState(false);
  // Speed belongs to the session, not the item. Resetting it every episode is
  // the behaviour people complain about in other players: you set 1.25x for a
  // slow talker and the next episode undoes it.
  const [speed, setSpeedState] = useState(1);
  const [shuffle, setShuffle] = useState(false);
  const [repeat, setRepeat] = useState<RepeatMode>("off");
  // The audio track to ask the server for, null meaning "whatever the file
  // leads with". Cleared when the item changes: a stream index is only
  // meaningful within one file.
  const [audioIndex, setAudioIndex] = useState<number | null>(null);

  const { data: item } = useItem(itemID);

  // A track has nothing to show, so its surface becomes the album's cover.
  // Cover art lives on the album row, not the track: the extraction worker
  // records one image per record rather than the same bytes once per song.
  const isAudio = item?.kind === "track";
  const { data: album } = useItem(isAudio ? (item?.parent_id ?? 0) : 0);
  const cover = artworkURL(
    album?.artwork?.poster ?? item?.artwork?.poster,
    "poster2x",
  );

  // Subtitles are a video affordance. Asking for a song's subtitle tracks is a
  // request that can only ever answer "none".
  const { data: subtitleData } = useSubtitles(itemID, !isAudio && itemID > 0);
  const subtitles = useMemo(() => subtitleData ?? [], [subtitleData]);

  const videoRef = useRef<HTMLVideoElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  const decision = useRef<Decision>({ method: "direct", reason: "" });
  const transcoding = useRef(false);
  // For a transcode, the element's clock is relative to this offset.
  const offset = useRef(0);
  const startedFrom = useRef(0);

  const [playing, setPlaying] = useState(false);
  const [current, setCurrent] = useState(0);
  const [duration, setDuration] = useState(0);
  // Mirrors the transcode offset (offset.current) as state, so the subtitle
  // track re-renders and re-fetches its cues shifted whenever the timeline's
  // zero point moves. Stays 0 for direct play.
  const [subOffset, setSubOffset] = useState(0);
  const [muted, setMuted] = useState(false);
  // Volume persists across titles and sessions: having to re-set it on every
  // playback is the kind of small friction that makes a player feel unfinished.
  const [volume, setVolume] = useState(() => {
    const saved = Number(localStorage.getItem("lancast:volume"));
    return Number.isFinite(saved) && saved > 0 && saved <= 1 ? saved : 1;
  });
  const [note, setNote] = useState("");
  const [subKey, setSubKey] = useState<string | null>(null);
  // True from the moment a source is chosen until the element has a frame to
  // show. A transcode takes seconds to produce its first bytes, and without
  // this the screen is black with a running clock — indistinguishable from a
  // playback that failed, which is the failure shape this project keeps
  // rediscovering.
  const [loading, setLoading] = useState(false);

  const activeSub =
    subtitles.find((t) => t.key === subKey && t.available) ?? null;

  // The player screen wins as soon as it is mounted, before anything is loaded.
  // Ordering it the other way round — item first, claim second — left the
  // surface at its 1px idle size for the first paints, so the screen above it
  // was transparent onto whatever page it had just covered: the detail page's
  // Play button showing through a "playing" player.
  const surface: Surface = fullClaimed ? "full" : itemID === 0 ? "idle" : "mini";

  // Total runtime. A transcode or remux streams a fragmented MP4 whose element
  // duration is whatever has been produced so far — a few seconds — so for those
  // the probed runtime is authoritative and the element's value is ignored.
  // Direct play trusts the element, which is exact for the actual file.
  const probedDuration = item?.duration_ms ? item.duration_ms / 1000 : 0;
  const totalDuration = transcoding.current
    ? probedDuration || duration
    : duration || probedDuration;

  const displayTime = transcoding.current ? offset.current + current : current;

  // ---- progress persistence -------------------------------------------------
  const lastSaved = useRef(0);
  const saveProgress = useCallback(
    (force = false) => {
      const now = Date.now();
      if (!force && now - lastSaved.current < 5000) return;
      lastSaved.current = now;
      const pos = transcoding.current ? offset.current + current : current;
      if (pos <= 0 || !itemID) return;
      void apiSend(`/api/items/${itemID}/progress`, "PUT", {
        position_ms: Math.floor(pos * 1000),
        watched: totalDuration ? pos / totalDuration > 0.92 : false,
      }).catch(() => {});
    },
    [current, itemID, totalDuration],
  );

  // The write that used to happen when the Player screen unmounted. It cannot
  // live there any more — leaving the screen is now a normal thing that does not
  // stop playback — so it happens when the item changes or the app closes.
  const saveRef = useRef(saveProgress);
  saveRef.current = saveProgress;
  useEffect(() => {
    const onHide = () => saveRef.current(true);
    window.addEventListener("pagehide", onHide);
    return () => {
      window.removeEventListener("pagehide", onHide);
      saveRef.current(true);
    };
  }, []);

  // ---- play / stop ----------------------------------------------------------
  const play = useCallback((id: number, q: number[]) => {
    setItemID((prev) => {
      // Re-entering the player screen for what is already playing must not
      // restart it: that is the whole point of the element outliving the route.
      if (prev === id) return prev;
      return id;
    });
    setQueue(q);
  }, []);

  const stop = useCallback(() => {
    saveRef.current(true);
    const v = videoRef.current;
    if (v) {
      v.pause();
      v.removeAttribute("src");
      v.load();
    }
    setItemID(0);
    setQueue([]);
    setPlaying(false);
    setLoading(false);
    setCurrent(0);
    setDuration(0);
  }, []);

  // "Play all" hands over the ordered ids of a container's children; the player
  // advances through them as each finishes. Advancing is internal now rather
  // than a route change, because it has to work with no player screen open.
  // The order the queue is actually played in. Shuffle is a *view* of the queue
  // rather than a rewrite of it: turning shuffle off has to give back the
  // original order, and a queue that had been shuffled in place could not.
  //
  // Seeded by the queue's own contents so it is stable for as long as that
  // queue is — re-shuffling on every render would make "next" mean something
  // different each time it was pressed.
  const order = useMemo(() => {
    if (!shuffle) return queue;
    const out = queue.slice();
    for (let i = out.length - 1; i > 0; i--) {
      const j = Math.floor(Math.random() * (i + 1));
      [out[i], out[j]] = [out[j], out[i]];
    }
    return out;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [shuffle, queue.join(",")]);

  const idxInOrder = order.indexOf(itemID);
  const hasNext = repeat !== "off" ? order.length > 1 : idxInOrder >= 0 && idxInOrder + 1 < order.length;
  const hasPrev = order.length > 1;

  // advanceQueue is what the *end of a track* calls. Repeat "one" is handled by
  // the caller, which reseeks rather than reloading the same source.
  // A stream index is only meaningful inside one file, so a new item starts
  // from the file's own default rather than carrying index 3 into something
  // that has two tracks.
  useEffect(() => {
    setAudioIndex(null);
  }, [itemID]);

  const advanceQueue = useCallback((): boolean => {
    const idx = order.indexOf(itemID);
    if (idx < 0) return false;
    if (idx + 1 < order.length) {
      setItemID(order[idx + 1]);
      return true;
    }
    // End of the queue. "all" wraps; "off" stops, which is what finishing an
    // album should do rather than starting it again.
    if (repeat === "all" && order.length > 0) {
      setItemID(order[0]);
      return true;
    }
    return false;
  }, [order, itemID, repeat]);

  const playNext = useCallback(() => {
    advanceQueue();
  }, [advanceQueue]);

  // Previous follows the convention every music player has taught people: near
  // the start of a track it goes back one, later it restarts the track. Without
  // it, "previous" three minutes into a song is a mis-press that loses your
  // place in the song you meant to keep.
  const playPrev = useCallback(() => {
    const v = videoRef.current;
    const elapsed = v ? offset.current + v.currentTime : 0;
    if (elapsed > 3) {
      seekTo(0);
      return;
    }
    const idx = order.indexOf(itemID);
    if (idx > 0) {
      setItemID(order[idx - 1]);
    } else if (repeat === "all" && order.length > 0) {
      setItemID(order[order.length - 1]);
    } else {
      seekTo(0);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [order, itemID, repeat]);

  const playFromQueue = useCallback((id: number) => setItemID(id), []);

  const toggleShuffle = useCallback(() => setShuffle((v) => !v), []);
  const cycleRepeat = useCallback(
    () => setRepeat((r) => (r === "off" ? "all" : r === "all" ? "one" : "off")),
    [],
  );

  // Speed is applied to the element rather than held as a wish: it has to be
  // re-applied after every source change, because a fresh <video> src resets
  // playbackRate to 1 — the same reason volume is re-applied on loadedmetadata.
  const setSpeed = useCallback((rate: number) => {
    setSpeedState(rate);
    const v = videoRef.current;
    if (v) v.playbackRate = rate;
  }, []);

  const audioTracks = useMemo(
    () => (item?.streams ?? []).filter((st) => st.kind === "audio"),
    [item?.streams],
  );

  // Choosing a track reloads the source, because the server decides delivery
  // from the track: a file that direct-plays with its first track may have to be
  // converted to deliver its second. Handled by the source effect, which this
  // re-triggers.
  const selectAudio = useCallback((index: number | null) => {
    setAudioIndex(index);
  }, []);

  // ---- source selection + resume -------------------------------------------
  useEffect(() => {
    if (!item) return;
    const v = videoRef.current;
    if (!v) return;
    let cancelled = false;

    startedFrom.current = item.progress?.position_ms
      ? item.progress.position_ms / 1000
      : 0;

    (async () => {
      try {
        const pb = await apiGet<{ decision: Decision }>(
          withCapabilities(`/api/items/${item.id}/playback`),
        );
        if (cancelled) return;
        decision.current = pb.decision;
      } catch {
        decision.current = { method: "direct", reason: "" };
      }
      transcoding.current = decision.current.method !== "direct";
      setNote(
        transcoding.current
          ? `${decision.current.method === "remux" ? "Repackaging" : "Converting"} — ${decision.current.reason}`
          : "",
      );
      if (transcoding.current) {
        offset.current = startedFrom.current;
        setSubOffset(startedFrom.current);
      } else {
        offset.current = 0;
        setSubOffset(0);
      }
      setLoading(true);
      v.src = sourceURL(
        item.id,
        decision.current.method,
        startedFrom.current,
        audioIndex,
      );
      v.load();
      void v.play().catch(() => {});
    })();

    return () => {
      cancelled = true;
      // Detaching the source ends a running transcode; leaving it attached keeps
      // ffmpeg alive after playback has moved on.
      v.pause();
      v.removeAttribute("src");
      v.load();
    };
    // Re-run when the item changes, and when a different audio track is asked
    // for — that is a new request to the server, not a client-side switch.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [item?.id, audioIndex]);

  // A direct-played file that fails is a capability this browser claimed and
  // does not have.
  //
  // `canPlayType` answers "probably" — HEVC in particular depends on the GPU
  // and, on Windows, on a codec extension that may not be installed. The claim
  // is dropped for this machine, remembered, and the file is asked for again;
  // the server, no longer told the client can decode it, converts it instead.
  //
  // Only for a *direct* source, and only for a claim not already withdrawn.
  // A transcode that fails is a server-side problem and retrying it under a
  // narrower profile would be the same request again — which is how a failing
  // file becomes an infinite loop rather than an error.
  const retryWithoutClaims = useCallback(() => {
    if (transcoding.current) return;
    const claimed = capabilities();
    if (!claimed) return;

    let news = false;
    for (const c of claimed.split(",")) {
      if (deny(c)) news = true;
    }
    if (!news) return;
    resetCapabilities();

    setNote("That file would not play directly — converting instead");
    const v = videoRef.current;
    if (!v || !item) return;
    decision.current = { method: "transcode", reason: "direct playback failed" };
    transcoding.current = true;
    offset.current = startedFrom.current;
    setSubOffset(startedFrom.current);
    setLoading(true);
    v.src = sourceURL(item.id, "transcode", startedFrom.current);
    v.load();
    void v.play().catch(() => {});
  }, [item]);

  // ---- seeking --------------------------------------------------------------
  const seekTo = useCallback(
    (target: number) => {
      const v = videoRef.current;
      if (!v) return;
      const t = Math.max(0, Math.min(target, totalDuration || target));
      if (transcoding.current) {
        offset.current = t;
        setSubOffset(t);
        setCurrent(0);
        setLoading(true);
        v.src = sourceURL(itemID, decision.current.method, t);
        v.load();
        void v.play().catch(() => {});
      } else {
        v.currentTime = t;
      }
      saveProgress(true);
    },
    [itemID, totalDuration, saveProgress],
  );

  const seekBy = useCallback(
    (delta: number) => seekTo(displayTime + delta),
    [seekTo, displayTime],
  );

  // ---- transport ------------------------------------------------------------
  const togglePlay = useCallback(() => {
    const v = videoRef.current;
    if (!v) return;
    if (v.paused) void v.play().catch(() => {});
    else v.pause();
  }, []);

  const toggleMute = useCallback(() => {
    const v = videoRef.current;
    if (!v) return;
    v.muted = !v.muted;
    setMuted(v.muted);
  }, []);

  /*
   * ---- MediaSession ---------------------------------------------------------
   *
   * What is playing, and where in it we are, told to the browser rather than
   * left to be inferred from the element.
   *
   * The element cannot be trusted for this. A transcode is a progressive fMP4
   * off a live pipe with no duration in its header, so `video.duration` is
   * whatever has been produced so far and grows a second per second — the same
   * reason `totalDuration` above prefers the probed runtime. Anything drawing
   * its own scrubber from the element inherits that lie: Windows' media overlay,
   * the media keys, and Chrome's picture-in-picture window, which is where it
   * was first noticed (0:12 and counting, on a 1h23m film).
   *
   * `setPositionState` is the only way to correct it without owning the window.
   * Whether Chrome's PiP scrubber honours it is the open question this is here
   * to answer; the OS-level controls are fixed by it either way, and they were
   * getting nothing at all before this.
   */
  useEffect(() => {
    const ms = navigator.mediaSession;
    if (!ms) return;

    if (!itemID || !item) {
      ms.metadata = null;
      ms.playbackState = "none";
      return;
    }

    ms.metadata = new MediaMetadata({
      title: item.title,
      // Read in the music sense on a track (ADR 0024): `series` is the album.
      artist: item.artist ?? "",
      album: item.series ?? "",
      artwork: cover ? [{ src: cover }] : [],
    });
    ms.playbackState = playing ? "playing" : "paused";
  }, [itemID, item, cover, playing]);

  useEffect(() => {
    const ms = navigator.mediaSession;
    if (!ms?.setPositionState) return;
    if (!itemID || !totalDuration) return;
    try {
      ms.setPositionState({
        duration: totalDuration,
        playbackRate: speed,
        // displayTime, not the element's clock: on a transcode the element
        // restarts at zero after every seek and the offset is what makes the
        // two agree.
        position: Math.min(displayTime, totalDuration),
      });
    } catch {
      // Chrome throws if position exceeds duration, which a rounding error at
      // the very end can produce. A stale position beats a dead player.
    }
  }, [itemID, totalDuration, displayTime, speed]);

  useEffect(() => {
    const ms = navigator.mediaSession;
    if (!ms?.setActionHandler) return;
    // Only what this player can actually do — an unhandled action makes the
    // browser hide that button, which is the same rule the control bar follows.
    const handlers: [MediaSessionAction, (() => void) | null][] = [
      ["play", togglePlay],
      ["pause", togglePlay],
      ["seekbackward", () => seekBy(-10)],
      ["seekforward", () => seekBy(10)],
      ["previoustrack", hasPrev ? playPrev : null],
      ["nexttrack", hasNext ? playNext : null],
    ];
    for (const [action, fn] of handlers) {
      try {
        ms.setActionHandler(action, fn);
      } catch {
        // Not every engine implements every action; an unsupported one throws
        // rather than being ignored, and it must not take the rest down.
      }
    }
    try {
      ms.setActionHandler("seekto", (d) => {
        if (typeof d.seekTime === "number") seekTo(d.seekTime);
      });
    } catch {
      /* no seekto on this engine */
    }
    return () => {
      for (const [action] of handlers) {
        try {
          ms.setActionHandler(action, null);
        } catch {
          /* as above */
        }
      }
      try {
        ms.setActionHandler("seekto", null);
      } catch {
        /* as above */
      }
    };
  }, [togglePlay, seekBy, seekTo, playNext, playPrev, hasNext, hasPrev]);

  // Apply and remember the level. Setting a level unmutes, which is what a user
  // dragging the slider up plainly means.
  const changeVolume = useCallback((next: number) => {
    const clamped = Math.min(1, Math.max(0, next));
    const v = videoRef.current;
    setVolume(clamped);
    localStorage.setItem("lancast:volume", String(clamped));
    if (!v) return;
    v.volume = clamped;
    if (clamped > 0 && v.muted) {
      v.muted = false;
      setMuted(false);
    }
  }, []);

  const toggleFullscreen = useCallback(() => {
    const el = containerRef.current;
    if (!el) return;
    if (document.fullscreenElement) void document.exitFullscreen();
    else void el.requestFullscreen().catch(() => {});
  }, []);

  // Cycle Off → each usable track → Off, for the [ and ] keys. Bitmap tracks
  // that cannot become WebVTT are skipped; they are pickable in the menu (with
  // their reason) but not worth cycling to.
  const cycleSub = useCallback(
    (dir: 1 | -1) => {
      const order: (string | null)[] = [
        null,
        ...subtitles.filter((t) => t.available).map((t) => t.key),
      ];
      const idx = order.indexOf(subKey);
      const next = (idx + dir + order.length) % order.length;
      setSubKey(order[next]);
    },
    [subtitles, subKey],
  );

  // A browser will not swap a <track> src reliably once parsed, so the active
  // track is keyed and remounted on change; here it is switched to showing.
  useEffect(() => {
    const v = videoRef.current;
    if (!v) return;
    for (const tt of v.textTracks) {
      tt.mode = activeSub ? "showing" : "disabled";
    }
    // subOffset is included because a transcode seek remounts the track with a
    // new offset-shifted src; the fresh track must be switched to showing too.
  }, [activeSub, subKey, subOffset]);

  const claimFullSurface = useCallback((claimed: boolean) => {
    setFullClaimed(claimed);
  }, []);

  const value: PlaybackState = {
    itemID,
    item,
    isAudio: !!isAudio,
    cover,
    surface,
    playing,
    loading,
    displayTime,
    totalDuration,
    muted,
    volume,
    note,
    subtitles,
    activeSub,
    subKey,
    audioTracks,
    audioIndex,
    speed,
    shuffle,
    repeat,
    queue,
    hasNext,
    hasPrev,
    play,
    stop,
    togglePlay,
    seekTo,
    seekBy,
    toggleMute,
    changeVolume,
    toggleFullscreen,
    cycleSub,
    selectSub: setSubKey,
    selectAudio,
    setSpeed,
    toggleShuffle,
    cycleRepeat,
    playNext,
    playPrev,
    playFromQueue,
    videoRef,
    containerRef,
    claimFullSurface,
  };

  return (
    <Ctx.Provider value={value}>
      {children}
      {/* One media element for the life of the app.
          It is moved between surfaces with CSS rather than re-parented: React
          would unmount and remount it on a move, and an unmounted element stops
          playing, which is the exact thing this provider exists to prevent. */}
      <div
        ref={containerRef}
        className={`playback playback--${surface}${isAudio ? " playback--audio" : ""}`}
      >
        {isAudio && (
          <div className="playback__cover" aria-hidden="true">
            {cover ? (
              <>
                <div
                  className="playback__cover-wash"
                  style={{ backgroundImage: `url(${cover})` }}
                />
                <img className="playback__cover-art" src={cover} alt="" />
              </>
            ) : (
              <div className="playback__cover-none">{item?.title}</div>
            )}
          </div>
        )}

        {/* The slot. The media element is its only child, and React never
            varies what is in here — everything conditional (the cover above,
            and whatever is added later) is a sibling of the slot rather than of
            the element.

            That is a hard requirement of the pop-out window in ADR 0029, not a
            tidying preference. Document picture-in-picture moves the element
            into another document imperatively, and while it is gone React must
            still be free to mount and unmount things around the player. Mount a
            conditional sibling *directly before* the element and React calls
            container.insertBefore(sibling, video) on a container the video has
            left: the DOM throws NotFoundError, in the commit phase, taking down
            the render pass. The cover was exactly that sibling.

            Inside a slot, the only insert that could use the element as an
            anchor is one inside the slot, and nothing else is ever in there.
            crossDocumentMove.test.tsx holds both shapes — this one proved safe,
            the old one kept as a characterisation test asserting it throws.

            display: contents, so the wrapper generates no box: the element's
            percentage sizing still resolves against .playback and no other rule
            in playback.css has to know this div exists. */}
        <div className="playback__slot">
          <video
            ref={videoRef}
            className={`playback__video${isAudio ? " playback__video--audio" : ""}`}
            playsInline
            onLoadedMetadata={(e) => {
              const v = e.currentTarget;
              if (isFinite(v.duration)) setDuration(v.duration);
              // The element resets to full volume on every new source, so the
              // remembered level has to be re-applied — including across a
              // transcode seek, which reloads the source.
              v.volume = volume;
              // Same reason as volume: a fresh source resets playbackRate to 1,
              // so a chosen speed has to be re-applied or it silently reverts on
              // the next episode.
              v.playbackRate = speed;
              // Resume: for direct play the element carries the offset itself.
              if (!transcoding.current && startedFrom.current > 0) {
                v.currentTime = startedFrom.current;
              }
              // A transcode seek reloads the source, which re-parses the <track>
              // at its default mode. Re-assert the selection so subtitles survive
              // a seek rather than silently switching off.
              for (const tt of v.textTracks) {
                tt.mode = activeSub ? "showing" : "disabled";
              }
            }}
            onLoadedData={() => setLoading(false)}
            // The note explains a wait ("Converting — audio codec ac3 is not
            // supported"). Once frames are arriving there is no wait left to
            // explain, and a permanent banner over the picture reads as a warning
            // about the thing you are currently watching happily. A later stall
            // shows the spinner on its own, which is the honest signal for it.
            onPlaying={() => {
              setLoading(false);
              setNote("");
            }}
            onWaiting={() => setLoading(true)}
            onError={() => retryWithoutClaims()}
            onTimeUpdate={(e) => {
              setCurrent(e.currentTarget.currentTime);
              saveProgress();
            }}
            onPlay={() => setPlaying(true)}
            onPause={() => {
              setPlaying(false);
              saveProgress(true);
            }}
            onEnded={() => {
              saveProgress(true);
              // Repeat one reseeks rather than reloading: the source is already
              // the right file, and re-requesting it would restart a transcode
              // that is already running.
              if (repeat === "one") {
                const v = e2(videoRef);
                if (v) {
                  v.currentTime = 0;
                  void v.play().catch(() => {});
                  return;
                }
              }
              // Roll on to the next queued item; if there is none, it ends here.
              if (!advanceQueue()) setPlaying(false);
            }}
          >
            {activeSub && (
              <track
                // Keyed by offset too: a transcode seek changes the media
                // timeline's zero point, so the cues must be re-fetched shifted to
                // match, or a resumed film shows no subtitles at all.
                key={`${activeSub.key}@${subOffset}`}
                kind="subtitles"
                default
                src={
                  `/api/items/${itemID}/subtitles/${encodeURIComponent(activeSub.key)}.vtt` +
                  (subOffset > 0 ? `?t=${subOffset}` : "")
                }
                srcLang={activeSub.language || undefined}
                label={activeSub.label}
              />
            )}
          </video>
        </div>
      </div>
    </Ctx.Provider>
  );
}

// e2 reads a ref inside a JSX handler without widening its type at every call
// site. Small, but it keeps the handler above readable.
function e2(ref: React.RefObject<HTMLVideoElement>): HTMLVideoElement | null {
  return ref.current;
}
