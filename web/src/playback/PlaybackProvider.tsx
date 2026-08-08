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
import type { Item, SubtitleTrack } from "@/api/types";

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
function sourceURL(id: number, method: Decision["method"], offset: number): string {
  if (method === "direct") return `/api/stream/${id}`;
  const t = offset > 0 ? `?t=${Math.floor(offset)}` : "";
  return `/api/stream/${id}/transcode${t}`;
}

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
  displayTime: number;
  totalDuration: number;
  muted: boolean;
  volume: number;
  note: string;

  subtitles: SubtitleTrack[];
  activeSub: SubtitleTrack | null;
  subKey: string | null;

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

  const activeSub =
    subtitles.find((t) => t.key === subKey && t.available) ?? null;

  const surface: Surface = itemID === 0 ? "idle" : fullClaimed ? "full" : "mini";

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
    setCurrent(0);
    setDuration(0);
  }, []);

  // "Play all" hands over the ordered ids of a container's children; the player
  // advances through them as each finishes. Advancing is internal now rather
  // than a route change, because it has to work with no player screen open.
  const advanceQueue = useCallback((): boolean => {
    const idx = queue.indexOf(itemID);
    if (idx < 0 || idx + 1 >= queue.length) return false;
    setItemID(queue[idx + 1]);
    return true;
  }, [queue, itemID]);

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
          `/api/items/${item.id}/playback`,
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
      v.src = sourceURL(item.id, decision.current.method, startedFrom.current);
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
    // Re-run only when the item identity changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [item?.id]);

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
    displayTime,
    totalDuration,
    muted,
    volume,
    note,
    subtitles,
    activeSub,
    subKey,
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
    </Ctx.Provider>
  );
}
