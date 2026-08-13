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
import {
  shuffledStartingWith,
  queueAfterEntry,
  resolvePos,
  nextPos,
  prevPos,
} from "./queueOrder";
import { usePrefs, qualityQuery, type Prefs } from "./prefs";

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
  quality?: string,
): string {
  // The chosen quality ceiling, or nothing at all for Original.
  //
  // It must ride on *every* request that participates in the delivery decision,
  // for the same reason ?audio= does: /playback and /transcode each decide from
  // their own parameters, so a seek that dropped the ceiling would be answered
  // about an uncapped stream — 409 "this file can be played directly" on a file
  // the ceiling is the only reason to touch, which kills the seek and with it
  // the playback.
  const cap = quality ? qualityQuery(quality) : "";
  // ?audio= names the absolute stream index (docs/api.md). It participates in
  // the delivery decision rather than only in stream selection, because a file
  // that direct-plays with its default track may need converting to deliver a
  // different one — a second audio track is often the one codec the browser
  // cannot decode.
  const a = audio != null ? `audio=${audio}` : "";
  if (method === "direct") {
    // A direct source is the file's own bytes; there is no encode to constrain,
    // and the server would not have said "direct" if the ceiling applied.
    return a ? `/api/stream/${id}?${a}` : `/api/stream/${id}`;
  }
  const parts = [offset > 0 ? `t=${Math.floor(offset)}` : "", a, cap].filter(
    Boolean,
  );
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
  /** The queue as it will actually play: shuffled when shuffle is on, and the
   *  same array as `queue` when it is off. Anything *showing* the queue must
   *  use this, or it describes an order that is not going to happen. */
  playOrder: number[];
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
  /** Whether the picture is filling the screen, however it got there. */
  fullscreen: boolean;
  /** Leave fullscreen if we are in it, by whichever mechanism put us there. */
  exitFullscreen: () => void;
  cycleSub: (dir: 1 | -1) => void;
  selectSub: (key: string | null) => void;
  selectAudio: (index: number | null) => void;
  setSpeed: (rate: number) => void;
  toggleShuffle: () => void;
  /** Set shuffle outright. A caller starting a randomised queue cannot use a
   *  toggle: it would turn shuffle *off* if it happened to be on already. */
  setShuffle: (on: boolean) => void;
  cycleRepeat: () => void;
  playNext: () => void;
  playPrev: () => void;
  /** `at` is the row's position, which is the only way to tell two copies of
   *  the same track apart in a playlist. */
  playFromQueue: (id: number, at?: number) => void;

  /** Device-local playback preferences; see prefs.ts. */
  prefs: Prefs;
  setPrefs: (patch: Partial<Prefs>) => void;

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

  // Device-local playback preferences (quality, output device, subtitle look,
  // auto play). See prefs.ts for why these are not per-user on the server.
  const [prefs, updatePrefs] = usePrefs();
  // Read inside callbacks that must not be rebuilt when the quality changes —
  // the source effect keys on the value itself so it reloads, but seekTo and
  // the failed-direct-play retry only need whatever is current at the moment
  // they fire. A dependency there would rebuild them on every settings change
  // for no behavioural difference.
  const qualityRef = useRef(prefs.quality);
  qualityRef.current = prefs.quality;
  // Same reasoning for the whole object, read by the `ended` handler.
  const prefsRef = useRef<Prefs>(prefs);
  prefsRef.current = prefs;

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
  // The <track> element currently mounted. Its .track is the one TextTrack
  // allowed to be showing; see the effect that enforces that below.
  const trackRef = useRef<HTMLTrackElement>(null);

  // The position the film is actually at, tagged with what it is the position
  // *in*. Kept as a ref because it is read by the source effect, which must not
  // re-run when it changes — it updates several times a second, and an effect
  // that reloads the source on it would reload the source forever.
  const livePos = useRef<{ id: number; at: number }>({ id: 0, at: 0 });

  const decision = useRef<Decision>({ method: "direct", reason: "" });
  const transcoding = useRef(false);
  // For a transcode, the element's clock is relative to this offset.
  const offset = useRef(0);
  const startedFrom = useRef(0);

  /*
   * Where in the order we are, as a position rather than an id.
   *
   * A playlist may hold the same track twice (ADR 0030), and an id cannot say
   * which copy is playing — indexOf always answers "the first one", so the
   * second copy resumed from the first and the rest of the queue was
   * unreachable. The id is still what plays; this is where it is playing from.
   *
   * Kept as a hint rather than as the source of truth: it is validated against
   * the order on every read (resolvePos), because shuffle and queue changes can
   * both leave it pointing at a different song.
   */
  const [pos, setPos] = useState(-1);

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
    // Not setQueue(q): re-entering the player for something already playing
    // inside a queue must not discard the queue. See queueAfterEntry.
    setQueue((prev) => queueAfterEntry(prev, q, id));
    // A new entry into the player has no opinion about position. Clearing it
    // makes the next read fall back to finding the item, rather than trusting
    // an index left over from a different queue.
    setPos(-1);
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
    // Starting with what is playing, so nothing shuffled in front of it is
    // stranded — see shuffledStartingWith.
    return shuffledStartingWith(queue, itemID);
    // itemID is deliberately absent: re-shuffling every time the track changes
    // would make "next" mean something different on each press. The order is
    // fixed when shuffle is turned on, or when the queue itself changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [shuffle, queue.join(",")]);

  const idxInOrder = resolvePos(order, pos, itemID);
  const hasNext = repeat !== "off" ? order.length > 1 : nextPos(order, idxInOrder, repeat) !== null;
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
    const from = resolvePos(order, pos, itemID);
    const to = nextPos(order, from, repeat);
    if (to == null) return false;
    setPos(to);
    setItemID(order[to]);
    return true;
  }, [order, pos, itemID, repeat]);

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
    const from = resolvePos(order, pos, itemID);
    const to = prevPos(order, from, repeat);
    if (to == null) {
      // Nothing before this one: restart it, which is what every player does.
      seekTo(0);
      return;
    }
    setPos(to);
    setItemID(order[to]);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [order, pos, itemID, repeat]);

  /*
   * Picked from the queue panel. The position comes with it, because the panel
   * is the one place that knows *which* row was pressed — and on a playlist
   * with a repeat, two rows carry the same id. Without the position, pressing
   * the second copy would start the first.
   */
  const playFromQueue = useCallback((id: number, at?: number) => {
    if (at != null) setPos(at);
    setItemID(id);
  }, []);

  const toggleShuffle = useCallback(() => setShuffle((v) => !v), []);
  const setShuffleMode = useCallback((on: boolean) => setShuffle(on), []);
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

    /*
     * Where to come back in.
     *
     * Normally the saved progress, which is what starting a film means. But
     * this effect also re-runs *during* playback — a different audio track, or
     * a different quality — and there the saved progress is up to five seconds
     * stale, because that is how often it is written. Reloading onto it would
     * rewind the film by a few seconds every time the settings panel was
     * touched, which reads as the player losing its place.
     *
     * So: the live position wins whenever we are already playing this same
     * item, and the saved one is for arriving at it.
     */
    const live = livePos.current;
    startedFrom.current =
      live.id === item.id && live.at > 0
        ? live.at
        : item.progress?.position_ms
          ? item.progress.position_ms / 1000
          : 0;

    (async () => {
      try {
        // The chosen audio track has to be part of the question. The decision
        // depends on it — a file that direct-plays with its own track may have
        // to be converted to deliver a different one — and asking without it
        // got an answer about the wrong track: "direct play", followed by a
        // request to /stream?audio=N, which serves the file's bytes and cannot
        // select anything. The browser then picked a track by its own rules and
        // the picker did nothing at all.
        // The quality ceiling is part of the question too, and for the same
        // reason the audio track is: it changes the answer. A 4K file the
        // client could decode direct-plays when nothing is capped and needs a
        // full encode at 720p, and asking without it gets an answer about a
        // stream that is not the one about to be requested.
        const ask = [
          audioIndex != null ? `audio=${audioIndex}` : "",
          qualityQuery(qualityRef.current),
        ].filter(Boolean);
        const pb = await apiGet<{ decision: Decision }>(
          withCapabilities(
            `/api/items/${item.id}/playback` +
              (ask.length > 0 ? `?${ask.join("&")}` : ""),
          ),
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
        qualityRef.current,
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
    // Re-run when the item changes, when a different audio track is asked for,
    // and when the quality ceiling moves — all three are new requests to the
    // server rather than client-side switches.
    //
    // Changing quality mid-film therefore restarts the source. It resumes at
    // the saved position, because that is what startedFrom reads, so what you
    // see is a short reconnect rather than a jump back to the beginning — the
    // same interruption a transcode seek already costs.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [item?.id, audioIndex, prefs.quality]);

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
    // Same reason as the seek below: the retry is about whichever track is
    // playing, not the file's default.
    v.src = sourceURL(
      item.id,
      "transcode",
      startedFrom.current,
      audioIndex,
      qualityRef.current,
    );
    v.load();
    void v.play().catch(() => {});
  }, [item, audioIndex]);

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
        // audioIndex has to travel with the seek. Without it the request is
        // about a different track than the one playing, and on a file whose
        // default track direct-plays the server answers 409 "this file can be
        // played directly" — so the seek dies, and with it the playback.
        v.src = sourceURL(
          itemID,
          decision.current.method,
          t,
          audioIndex,
          qualityRef.current,
        );
        v.load();
        void v.play().catch(() => {});
      } else {
        v.currentTime = t;
      }
      saveProgress(true);
    },
    [itemID, totalDuration, saveProgress, audioIndex],
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

  /*
   * Fullscreen takes the whole document, not the media surface.
   *
   * The surface below holds the media element and nothing else — the player's
   * controls are a sibling of it, in the screen that `children` renders. So
   * asking the *surface* to go fullscreen displayed the video and hid every
   * control, which is what "the player breaks and all the buttons disappear"
   * was: not a fault at all, but fullscreen doing exactly what it was told.
   *
   * The document element contains both, and the player screen is already a
   * fixed overlay filling the viewport, so fullscreening it changes what the
   * viewport *is* and nothing about the layout. It also keeps the mousemove
   * that wakes the controls inside the fullscreened subtree, which fullscreening
   * the surface did not: the controls could not have come back even if they had
   * been visible.
   */
  /*
   * Whether the picture is filling the screen.
   *
   * Tracked here rather than read from document.fullscreenElement, because in
   * the desktop client there is no fullscreen element: the *window* is
   * borderless and the size of a monitor, and the page knows nothing about it.
   * Without this the control could never show its state, and Escape had nothing
   * to ask.
   */
  const [fullscreen, setFullscreen] = useState(false);

  // The browser's own exits — Escape, F11, the window manager — happen without
  // asking us, so the flag follows the document rather than only the button.
  useEffect(() => {
    const sync = () => setFullscreen(Boolean(document.fullscreenElement));
    document.addEventListener("fullscreenchange", sync);
    return () => document.removeEventListener("fullscreenchange", sync);
  }, []);

  const exitFullscreen = useCallback(() => {
    const host = (window as { lancastToggleFullscreen?: () => Promise<boolean> })
      .lancastToggleFullscreen;
    if (host) {
      // The binding is a toggle, so this asks only when there is something to
      // leave — calling it blind would put a windowed player into fullscreen,
      // which is the opposite of what Escape means.
      if (fullscreen) void host().then((on) => setFullscreen(Boolean(on)));
      return;
    }
    if (document.fullscreenElement) void document.exitFullscreen().catch(() => {});
  }, [fullscreen]);

  const toggleFullscreen = useCallback(() => {
    /*
     * In the LANcast window, fullscreen is the *window's* job.
     *
     * WebView2 hands "this page wants fullscreen" to its host and does nothing
     * itself, and the host has to resize, drop the frame and cover the taskbar.
     * So requestFullscreen() in the desktop client left the page believing it
     * was fullscreen inside a window that had not changed size — a button that
     * appears to do nothing. The host exposes a binding that does the real
     * thing, on the monitor the window is actually on.
     *
     * In a browser there is no binding and the Fullscreen API is right.
     */
    const host = (window as { lancastToggleFullscreen?: () => Promise<boolean> })
      .lancastToggleFullscreen;
    if (host) {
      // The binding answers with the state it ended in, which is the only
      // source of truth for a window the page cannot see.
      void host().then((on) => setFullscreen(Boolean(on)));
      return;
    }
    if (document.fullscreenElement) {
      void document.exitFullscreen().catch(() => {});
      return;
    }
    void document.documentElement.requestFullscreen().catch(() => {});
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

  /*
   * A browser will not swap a <track> src reliably once parsed, so the active
   * track is keyed and remounted on change; here it is switched to showing.
   *
   * Exactly one track may be showing, and the list is not only ever one long.
   * Removing a <track> element is supposed to drop its TextTrack from
   * video.textTracks, and in practice the entry outlives the element — so after
   * switching subtitles once, the list holds the old track as well as the new.
   * This used to set *every* entry in it to `showing`, which turned the stale
   * one back on: two tracks rendering at once, their cues stacking up the
   * screen and never clearing, with duplicated lines wherever two files
   * translate the same dialogue. It is at its worst with two downloads of the
   * same language, which is the ordinary way of trying to find a good one.
   *
   * So: everything off, then ours on, identified by the element rather than by
   * position or label — a second English file has the same label and cannot be
   * told apart by one.
   */
  useEffect(() => {
    const v = videoRef.current;
    if (!v) return;
    for (const tt of v.textTracks) {
      tt.mode = "disabled";
    }
    const own = trackRef.current?.track;
    if (activeSub && own) {
      own.mode = "showing";
    }
    // subOffset is included because a transcode seek remounts the track with a
    // new offset-shifted src; the fresh track must be switched to showing too.
  }, [activeSub, subKey, subOffset]);

  /*
   * ---- subtitle timing offset -----------------------------------------------
   *
   * Applied to the parsed cues here rather than by asking the server for a
   * shifted file, for three reasons. It is instant, where a refetch is a
   * visible gap in the subtitles every time the control moves — and this is a
   * control people nudge repeatedly until it looks right, so the gap would land
   * on every press. It works in both directions, where the server's ShiftVTT
   * only moves cues *earlier* and drops what falls off the front, which is
   * correct for its actual job (lining a file up with a transcode's zero point)
   * and wrong as a user control. And it composes with that job rather than
   * fighting it: ?t= still handles the transcode shift, and this rides on top.
   *
   * The applied amount is tracked because cues are mutated in place. Shifting
   * by the preference each time the effect ran would shift by it again on every
   * render; the delta is what makes it idempotent. A remounted <track> parses
   * fresh cues, so the applied amount resets with it.
   */
  const appliedOffset = useRef(0);
  useEffect(() => {
    const el = trackRef.current;
    const tt = el?.track;
    if (!el || !tt) return;

    const apply = () => {
      const cues = tt.cues;
      if (!cues) return;
      const delta = prefs.subOffset - appliedOffset.current;
      if (delta === 0) return;
      for (let i = 0; i < cues.length; i++) {
        const c = cues[i];
        // Never past zero: a negative start time is not a cue that shows
        // earlier, it is a cue the engine drops.
        c.startTime = Math.max(0, c.startTime + delta);
        c.endTime = Math.max(0, c.endTime + delta);
      }
      appliedOffset.current = prefs.subOffset;
    };

    // Cues are parsed asynchronously; an offset set before the file has loaded
    // would find an empty list and silently do nothing.
    if (tt.cues && tt.cues.length > 0) apply();
    el.addEventListener("load", apply);
    return () => el.removeEventListener("load", apply);
    // subOffset and the track key are in here because both remount the element,
    // which resets the cues to their unshifted state.
  }, [prefs.subOffset, subKey, subOffset, activeSub]);

  // A remount means fresh cues, so nothing has been applied to them yet.
  useEffect(() => {
    appliedOffset.current = 0;
  }, [subKey, subOffset]);

  /*
   * ---- audio output device --------------------------------------------------
   *
   * setSinkId is not universal — Firefox has it behind a pref and Safari has
   * none — so this is best-effort by design: an engine without it plays out of
   * the system default, which is what it did before this existed. The settings
   * panel hides the row rather than offering a control that cannot do anything.
   *
   * Re-applied on every source change as well as on the choice itself: a fresh
   * src does not reset the sink in the engines that implement it, but a *new
   * element* would, and the pop-out window in ADR 0029 moves this element
   * between documents. Cheap enough to simply re-assert.
   */
  useEffect(() => {
    const v = videoRef.current as (HTMLVideoElement & {
      setSinkId?: (id: string) => Promise<void>;
    }) | null;
    if (!v?.setSinkId) return;
    v.setSinkId(prefs.audioDevice).catch(() => {
      // A device that has been unplugged since it was chosen. Falling back to
      // the default is what the engine does anyway, and there is nothing useful
      // to say about it mid-film.
    });
  }, [prefs.audioDevice, itemID, subOffset]);

  /*
   * ---- subtitle appearance --------------------------------------------------
   *
   * Written as CSS custom properties on the document, because ::cue cannot be
   * styled inline — the cue box lives in a shadow tree the page cannot reach
   * with a style attribute, and the only hook is a stylesheet rule that reads
   * these. The rule itself is in playback.css.
   *
   * Position is a percentage from the bottom of the picture. It moves the cue
   * box rather than the text inside it, which is why it is a `bottom` on the
   * container and not something ::cue could express at all.
   */
  useEffect(() => {
    const s = document.documentElement.style;
    s.setProperty("--cue-color", prefs.subColor);
    s.setProperty("--cue-scale", String(prefs.subSize));
    s.setProperty("--cue-bottom", `${prefs.subPosition}%`);
  }, [prefs.subColor, prefs.subSize, prefs.subPosition]);

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
    playOrder: order,
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
    fullscreen,
    exitFullscreen,
    cycleSub,
    selectSub: setSubKey,
    selectAudio,
    setSpeed,
    toggleShuffle,
    setShuffle: setShuffleMode,
    cycleRepeat,
    playNext,
    playPrev,
    playFromQueue,
    prefs,
    setPrefs: updatePrefs,
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
              // a seek rather than silently switching off — and re-assert it the
              // same way the effect above does, one track showing and every
              // other one off. Setting the whole list to showing here would put
              // a stale track back on screen at the first seek even once the
              // effect had cleared it.
              for (const tt of v.textTracks) {
                tt.mode = "disabled";
              }
              const own = trackRef.current?.track;
              if (activeSub && own) {
                own.mode = "showing";
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
              const t = e.currentTarget.currentTime;
              setCurrent(t);
              // What a reload should come back in at. offset.current is zero on
              // direct play and the transcode's own zero point otherwise, which
              // is the same sum displayTime makes.
              livePos.current = {
                id: itemID,
                at: (transcoding.current ? offset.current : 0) + t,
              };
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
              /*
               * Auto play, and what it does *not* cover.
               *
               * Only the end of a track consults it. Pressing Next is an
               * explicit request to move on and must work regardless — a
               * setting that disabled the next button would be a broken
               * control, not an honoured preference. Repeat one is likewise
               * above this: it is a loop the user asked for, not an advance.
               *
               * With it off the queue stays intact and the position stays put,
               * so pressing play again resumes into it. Clearing the queue here
               * would make "don't roll on automatically" mean "throw away the
               * album", which is not what it says.
               */
              if (!prefsRef.current.autoPlay) {
                setPlaying(false);
                return;
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
                ref={trackRef}
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
