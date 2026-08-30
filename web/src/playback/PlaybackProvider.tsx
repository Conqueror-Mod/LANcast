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
import { createPortal } from "react-dom";
import { useAuthStatus, useItem, useSubtitles } from "@/api/hooks";
import { apiGet, apiSend, artworkURL } from "@/api/client";
import type { Item, SubtitleTrack, MediaStream } from "@/api/types";
import {
  withCapabilities,
  capabilities,
  capabilitiesNeededBy,
  deny,
  resetCapabilities,
} from "./capabilities";
import {
  NO_RUN,
  advanced,
  attended,
  describeRun,
  shouldAsk,
  type WatchRun,
} from "./stillWatching";
import { resumeSeconds, startedFloorMs } from "./resumePoint";
import {
  shuffledStartingWith,
  queueAfterEntry,
  resolvePos,
  nextPos,
  prevPos,
} from "./queueOrder";
import { usePrefs, qualityQuery, type Prefs } from "./prefs";
import { popoutSupported, openPopout, moveElement } from "./popout";
import { PopoutPlayer } from "./PopoutPlayer";

import { conversionHelp } from "./conversionAvailable";
import {
  filePath,
  hlsWorthTrying,
  isUnsupportedSource,
  rememberHLS,
  type FilePath,
} from "./fileTransport";
import { mediaCapability } from "@/lib/liveTransport";
import { struggling, type Sample } from "./decodeHealth";
/*
 * What to say during the wait, in words written for the person waiting.
 *
 * This used to append the decision's own `reason` verbatim, which is a sentence
 * written for a log: "Repackaging — matroska container is not supported, but
 * both codecs are". Every word of that is true and it reads as a complaint
 * about the file, so a viewer asked why their MKV was unsupported when nothing
 * was wrong — the server was doing the cheapest thing it can do, rewriting the
 * container while copying both streams untouched.
 *
 * The reason has not gone anywhere. It is in the server log and on the activity
 * panel, which is where a sentence in that vocabulary belongs. What a viewer
 * needs is how long to expect to wait and why there is a wait at all.
 */
export function waitNote(d: { method: string; reason?: string }): string {
  if (d.method === "remux") {
    // A container rewrite copies both streams, so it is quick and lossless.
    // Naming that is the difference between "my file is unsupported" and "it
    // is being put in a different box".
    return "Repackaging for your browser — this is quick, and nothing is re-encoded";
  }
  return "Converting for your browser — this can take a few seconds to start";
}

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
/*
 * How near the probed runtime counts as "the end".
 *
 * A transcode's own timeline and the probed duration disagree by a little —
 * container rounding, a trailing partial fragment — so a genuine end lands a
 * second or two short of the number. Ten seconds is comfortably past that and
 * still far short of a pause, which cuts the stream wherever the viewer left
 * it. It doubles as the tolerance for "cut at the same place twice".
 */
const TRUNCATION_SLACK = 10;

function sourceURL(
  id: number,
  method: Decision["method"],
  offset: number,
  audio?: number | null,
  quality?: string,
  path: FilePath = "progressive",
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
  /*
   * A playlist instead of one endless response, where the engine can take it.
   *
   * Same item, same parameters — `t`, `audio` and the quality ceiling all
   * participate in the delivery decision on this endpoint exactly as they do on
   * /transcode, so a playlist asking for less than the decision was made with
   * gets a different answer, or a 409.
   *
   * What changes is what eviction costs. See fileTransport.ts: this is the fix
   * for a stream that could only ever be re-asked from byte zero.
   */
  if (path === "hls") {
    return withCapabilities(`/api/stream/${id}/hls/index.m3u8${t}`);
  }
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
  /*
   * "Are you still watching": the text to show, or null.
   *
   * A string rather than a boolean so the prompt can say what the machine
   * actually did — "3 things have played automatically over about 2 hours" is
   * a fact about the queue, where "are you still there?" is a question about
   * the person, and only one of those is the player's business.
   */
  stillWatching: string | null;
  /** Answering it: resume, and start the run over. */
  keepWatching: () => void;
  playNext: () => void;
  /** Items queued by hand, played before the queue resumes. */
  upNext: number[];
  /** Queue an item to play immediately after the current one. */
  playNextUp: (id: number) => void;
  /** Queue an item behind anything already queued by hand. */
  addToQueue: (id: number) => void;
  /** Drop one entry from the hand-queued lane, by position. */
  removeFromUpNext: (at: number) => void;
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

  /*
   * Pop-out: our own always-on-top window, not the browser's (ADR 0029).
   *
   * `popoutAvailable` is a feature test, not a preference — Document PiP is
   * Chromium-only, and a button that cannot act is not shown. `popout` is
   * whether the window is open right now, which the player screen reads so its
   * own surface can say where the picture went.
   */
  popoutAvailable: boolean;
  popout: boolean;
  togglePopout: () => void;
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
  /*
   * Items queued by hand, played before the queue resumes.
   *
   * A lane of its own rather than an insertion into `queue`, and the reason is
   * written into the shuffle memo below: shuffle is "a view of the queue rather
   * than a rewrite of it", and that view is rebuilt whenever the queue's
   * contents change. Splicing a track into `queue` would therefore reshuffle
   * everything else as a side effect of adding one thing — press "play next" on
   * a shuffled library and the other 1,590 tracks reorder underneath you.
   *
   * It is also the more useful model. "Add to the end of the queue" means
   * nothing when the queue is a whole shuffled library; "after this one" is
   * what people are actually asking for, which is why every player that has
   * both keeps them apart.
   *
   * Empty is the normal state, and while it is empty nothing here changes how
   * the queue behaves at all.
   */
  const [upNext, setUpNext] = useState<number[]>([]);
  /*
   * The unattended run, for "are you still watching".
   *
   * A ref rather than state because every write happens inside an event
   * handler that is already doing something else, and re-rendering the player
   * to record that the queue moved on would be a render per episode for a
   * value nothing draws. What is drawn is the prompt, and that is state.
   */
  const runRef = useRef<WatchRun>(NO_RUN);
  const [stillWatching, setStillWatching] = useState<string | null>(null);
  /*
   * The prompt, readable from an event handler without re-subscribing it.
   *
   * `onPause` must not clear the run when the pause is the one this feature
   * performs itself — that fires immediately after the prompt is set, and
   * would wipe it on the way past.
   */
  const stillWatchingRef = useRef<string | null>(null);
  /*
   * True while the item playing came out of that lane.
   *
   * `pos` is a cursor into `order`, and an item played out of band is not at
   * that cursor — resolvePos would fall back to searching for it and find
   * either nothing or the wrong copy. So the cursor stays where the queue left
   * off and this remembers that we are away from it, which is what lets the
   * queue resume from the right place afterwards rather than from wherever the
   * queued track happened to sit.
   */
  const [offPiste, setOffPiste] = useState(false);
  const [shuffle, setShuffle] = useState(false);
  /*
   * Which randomisation this is.
   *
   * The shuffled order is memoised on the queue and the shuffle flag, which is
   * right for "next must mean the same thing on every press" — and wrong for
   * "Randomize all", pressed twice. That hands over the *same* library in the
   * *same* order with shuffle already on, so neither dependency changes, the
   * memo is reused, and the button produces the identical running order every
   * time it is pressed. It looked like one randomisation baked in at startup.
   *
   * The epoch is the missing dependency: asking for shuffle bumps it, so a
   * fresh request reshuffles while nothing else does.
   */
  const [shuffleEpoch, setShuffleEpoch] = useState(0);
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
  /*
   * Which delivery the current source is using, so a failure can be attributed.
   *
   * The element reports "this did not work" and nothing about which of the two
   * things it was handed. Without this, a playlist an engine cannot read is
   * indistinguishable from a transcode the server refused, and the two want
   * opposite responses — one retries down a different path, the other must not
   * retry at all (see transcodeFailed).
   */
  const chosenPath = useRef<FilePath>("progressive");
  // For a transcode, the element's clock is relative to this offset.
  const offset = useRef(0);
  const startedFrom = useRef(0);
  /*
   * Where the last truncation recovery was attempted, so it cannot loop.
   *
   * Recovery re-requests the transcode at the position the stream was cut at.
   * If the new stream is cut at the same place — a file ffmpeg genuinely cannot
   * get past — repeating that is an infinite reload rather than playback, so
   * the second attempt at the same spot gives up and lets the queue advance.
   */
  const recoveredAt = useRef<number | null>(null);

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
  /*
   * The latest auth status, held in a ref.
   *
   * Read inside an async effect that runs once per item, and a value captured
   * in its closure would be whatever was true when the effect started. A ref
   * is the only shape that gives the check the *current* answer — which matters
   * because installing ffmpeg mid-session is exactly the fix this message is
   * telling somebody to apply.
   */
  const auth = useAuthStatus().data;
  const authRef = useRef(auth);
  authRef.current = auth;
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
  const surface: Surface = fullClaimed
    ? "full"
    : itemID === 0
      ? "idle"
      : "mini";

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
      /*
       * A position this early is not a bookmark, and writing one does damage.
       *
       * This throttle fires every five seconds, so skipping through a shuffled
       * library to find something to watch wrote a five-second position onto
       * every film passed over. Each then resumed at 0:05 *and* appeared on the
       * Continue Watching shelf, which is the shelf claiming you are part way
       * through forty films you glanced at.
       *
       * resumeSeconds refuses to resume from under the same floor, which fixes
       * the positions already in the database. This is the other half: stop
       * making them. Both are needed — the read side alone leaves the Continue
       * shelf wrong, because that is a server query over position_ms and never
       * goes near resumeSeconds.
       *
       * The finished check stays outside it. Something at 0.92 of its duration
       * is finished however short it is, and a five-minute item reaches that
       * before it reaches the floor.
       */
      const done = totalDuration ? pos / totalDuration > 0.92 : false;
      if (!done && pos * 1000 < startedFloorMs(totalDuration * 1000)) return;
      void apiSend(`/api/items/${itemID}/progress`, "PUT", {
        position_ms: Math.floor(pos * 1000),
        watched: done,
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
    setQueue((prev) => {
      const next = queueAfterEntry(prev, q, id);
      /*
       * A genuinely new queue abandons anything queued against the old one.
       * Tracks lined up behind an album are about that album; carrying them
       * into a film somebody has just started is a queue that surprises you.
       * Re-entering the player for what is already playing keeps them, which
       * is the same test queueAfterEntry makes for the queue itself.
       */
      if (next !== prev) {
        setUpNext([]);
        setOffPiste(false);
      }
      return next;
    });
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
    setUpNext([]);
    setOffPiste(false);
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
  }, [shuffle, shuffleEpoch, queue.join(",")]);

  const idxInOrder = resolvePos(order, pos, itemID);
  // Something queued by hand is a next, whatever the queue behind it says —
  // including when the queue is a single item and would otherwise end here.
  const hasNext =
    upNext.length > 0 ||
    (repeat !== "off"
      ? order.length > 1
      : nextPos(order, idxInOrder, repeat) !== null);
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
    /*
     * The hand-queued lane comes first, and takes the cursor with it.
     *
     * `pos` stays where the queue left off rather than following the item that
     * plays, because the queued item is not in `order` at that index — it may
     * not be in `order` at all. Leaving the cursor put is what lets the queue
     * resume from the right place once the lane drains, instead of from
     * wherever the queued thing happened to sit.
     */
    if (upNext.length > 0) {
      const [head, ...rest] = upNext;
      /*
       * The cursor is made concrete on the way out, not left to be derived.
       *
       * `pos` is -1 for most of a queue's life — play() clears it and every
       * read goes through resolvePos, which recovers the index from the item
       * that is playing. That recovery stops working the moment the item
       * playing is a queued one that is not in the order, so the index has to
       * be resolved here, while the answer is still knowable, or the queue
       * resumes from nowhere and advancing answers null.
       */
      setPos(resolvePos(order, pos, itemID));
      setUpNext(rest);
      setOffPiste(true);
      setItemID(head);
      return true;
    }
    /*
     * Off the lane, the cursor is trusted as-is: resolvePos would look for the
     * item that just played, and the item that just played was a queued one
     * that is not at `pos`, so it would answer with the wrong index or none.
     */
    const from = offPiste ? pos : resolvePos(order, pos, itemID);
    const to = nextPos(order, from, repeat);
    if (to == null) return false;
    setOffPiste(false);
    setPos(to);
    setItemID(order[to]);
    return true;
  }, [order, pos, itemID, repeat, upNext, offPiste]);

  /*
   * Answering the prompt is the clearest attention there is, so it buys a full
   * new run rather than a few quiet minutes. Anything less and the prompt
   * returns almost at once, which is how this feature becomes the nuisance it
   * exists to avoid.
   */
  /*
   * Anything deliberate clears the unattended run.
   *
   * The rule in stillWatching.ts says "any deliberate act resets it" and only
   * Next and the prompt actually did, which left the feature able to interrupt
   * somebody who had plainly just been at the remote — pausing to see what was
   * on, or skipping back thirty seconds, both left the count climbing.
   *
   * Deliberately not called from `timeupdate`, which fires into an empty room,
   * or from `waiting`, which is the network rather than a person.
   */
  const noteAttention = useCallback(() => {
    runRef.current = attended();
    stillWatchingRef.current = null;
    setStillWatching(null);
  }, []);

  const keepWatching = useCallback(() => {
    runRef.current = attended();
    stillWatchingRef.current = null;
    setStillWatching(null);
    advanceQueue();
  }, [advanceQueue]);

  const playNext = useCallback(() => {
    // Pressing Next is attention, like every other deliberate act.
    runRef.current = attended();
    stillWatchingRef.current = null;
    setStillWatching(null);
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

  // Turning shuffle *on* is a new randomisation; turning it off is not, and
  // bumping the epoch there would only reshuffle an order nobody is using.
  /*
   * Queue something by hand.
   *
   * `next` puts it at the front of the lane, `last` at the back — which is what
   * "play next" and "add to queue" mean when both exist. Neither touches
   * `queue`, so nothing reshuffles and turning shuffle off still gives back the
   * order it always would have.
   *
   * With nothing playing there is no "next" to be after, so it just plays. A
   * queue action that silently did nothing on an idle player would be the kind
   * of control people press twice and then distrust.
   */
  const enqueue = useCallback(
    (id: number, where: "next" | "last") => {
      if (itemID === 0) {
        setItemID(id);
        setQueue([id]);
        setPos(-1);
        setOffPiste(false);
        return;
      }
      setUpNext((u) => (where === "next" ? [id, ...u] : [...u, id]));
    },
    [itemID],
  );
  /*
   * Take something back off the lane.
   *
   * By position, not by id: the lane can hold the same track twice, on purpose
   * — queueing a song again is a thing people do — and removing "the one with
   * that id" would take the wrong copy. The same reasoning ADR 0030 forced on
   * playlists.
   *
   * Only the lane. Removing from `queue` itself would change its contents and
   * so rebuild the shuffled order, reordering everything else as a side effect
   * of dropping one row — the same reason inserting into it was rejected.
   */
  const removeFromUpNext = useCallback((at: number) => {
    setUpNext((u) => u.filter((_, i) => i !== at));
  }, []);

  const playNextUp = useCallback(
    (id: number) => enqueue(id, "next"),
    [enqueue],
  );
  const addToQueue = useCallback(
    (id: number) => enqueue(id, "last"),
    [enqueue],
  );

  const toggleShuffle = useCallback(() => {
    setShuffle((v) => {
      if (!v) setShuffleEpoch((n) => n + 1);
      return !v;
    });
  }, []);
  // Always a new randomisation, including when shuffle is already on: this is
  // the path "Randomize all" takes, and pressing it is a request for a
  // different order, not a request for the state it is already in.
  const setShuffleMode = useCallback((on: boolean) => {
    setShuffle(on);
    if (on) setShuffleEpoch((n) => n + 1);
  }, []);
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
    /*
     * A finished item starts again rather than resuming past its own end.
     *
     * The saved position of a watched episode lands *after* the final frame, so
     * resuming there fired `ended` on the first tick and the queue advanced —
     * press play on episode one of a show you are part way through and episode
     * three played, having walked through the finished ones too fast to see.
     */
    // A new source is a new stream; whatever the last one was cut at says
    // nothing about this one.
    recoveredAt.current = null;
    startedFrom.current =
      live.id === item.id && live.at > 0
        ? live.at
        : resumeSeconds({
            positionMs: item.progress?.position_ms,
            watched: item.progress?.watched,
            durationMs: item.duration_ms,
          });

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

      /*
       * Say it before trying, not after failing (ADR 0048).
       *
       * A server without ffmpeg answers this request perfectly well — the
       * decision is computed from stored probe data — and then cannot carry it
       * out. Left alone, the element is handed a request that will be refused,
       * reports a bare error with no status, and the viewer sees a black
       * rectangle. That is how somebody concludes the software cannot play
       * their library rather than that a tool is missing.
       *
       * Checked here because this is the last moment before the attempt, and
       * because the answer is already in hand: every screen fetches auth status,
       * which reports whether the server can convert at all.
       */
      const help = conversionHelp(
        authRef.current?.can_convert,
        authRef.current?.user?.role,
        transcoding.current ? "file" : "no",
      );
      if (help) {
        setLoading(false);
        setNote(`${help.title}. ${help.action}`);
        return;
      }

      setNote(transcoding.current ? waitNote(decision.current) : "");
      if (transcoding.current) {
        offset.current = startedFrom.current;
        setSubOffset(startedFrom.current);
      } else {
        offset.current = 0;
        setSubOffset(0);
      }
      setLoading(true);
      chosenPath.current = filePath(
        decision.current.method,
        hlsWorthTrying(mediaCapability().canPlayType),
      );
      v.src = sourceURL(
        item.id,
        decision.current.method,
        startedFrom.current,
        audioIndex,
        qualityRef.current,
        chosenPath.current,
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

  /*
   * A direct-played file that fails is a capability this browser claimed and
   * does not have — but only one of them, and only one of the ones this file
   * actually used.
   *
   * `canPlayType` answers "probably", never "definitely": HEVC in particular
   * depends on the GPU and, on Windows, on a codec extension that may not be
   * installed. So a failed direct play withdraws the claim that produced it,
   * and the server converts the file next time instead.
   *
   * **It used to withdraw every claim at once**, because the element's error
   * does not say which codec let it down. One TrueHD file that no browser can
   * decode therefore took hevc, hevc10, high10, ac3, eac3, flacmp4 and opusmp4
   * with it, and the server re-encoded everything until they expired. Measured
   * on the reporting install: 130 transcode sessions whose stated reason was
   * `video codec hevc is not supported`, on a machine that decodes HEVC in
   * hardware. `capabilities.ts` already described that state — "every claim the
   * client is capable of making", all false — and read it as a problem of
   * permanence, which shortened the damage without touching the cause.
   *
   * A file cannot be ruined by a claim it never needed, so only the claims its
   * own streams called for are at risk.
   *
   * The conversion is no longer conditional on having withdrawn something. Those
   * are two different jobs: withdrawing a claim is what this *remembers*, and
   * converting is what it *does about the film in front of somebody*. A failure
   * with no attributable claim — an unreadable file, a codec nothing here
   * models — used to produce a bare error and no recovery at all.
   *
   * Only for a *direct* source. A transcode that fails is a server-side problem
   * and retrying it under a narrower profile would be the same request again,
   * which is how a failing file becomes an infinite loop rather than an error.
   */
  const retryWithoutClaims = useCallback(() => {
    if (transcoding.current) return;

    const claimed = capabilities();
    if (claimed) {
      const atRisk = capabilitiesNeededBy(item?.streams);
      const claims = claimed.split(",");
      const suspects = atRisk.filter((c) => claims.includes(c));
      let news = false;
      for (const c of suspects) {
        if (deny(c)) news = true;
      }
      if (news) resetCapabilities();
    }

    setNote("That file would not play directly — converting instead");
    const v = videoRef.current;
    if (!v || !item) return;
    decision.current = {
      method: "transcode",
      reason: "direct playback failed",
    };
    transcoding.current = true;
    offset.current = startedFrom.current;
    setSubOffset(startedFrom.current);
    setLoading(true);
    // Same reason as the seek below: the retry is about whichever track is
    // playing, not the file's default.
    chosenPath.current = filePath(
      "transcode",
      hlsWorthTrying(mediaCapability().canPlayType),
    );
    v.src = sourceURL(
      item.id,
      "transcode",
      startedFrom.current,
      audioIndex,
      qualityRef.current,
      chosenPath.current,
    );
    v.load();
    void v.play().catch(() => {});
  }, [item, audioIndex]);

  /*
   * ---- a direct play that is not coping -------------------------------------
   *
   * Measured on the reporting install: two HEVC Main 10 films dropped 19.8% and
   * 19.9% of their frames while an H.264 film from the same folder, minutes
   * later, dropped none. Reported as heavy frame lag, and invisible to
   * everything that could have predicted it — `canPlayType` says "probably" and
   * `mediaCapabilities.decodingInfo()` says smooth *and* power-efficient for
   * the exact shape of the file that fails.
   *
   * So it is caught by watching. The counters are sampled while the element is
   * genuinely playing, and the baseline is dropped whenever it is not: a paused
   * or rebuffering element drops frames for reasons that are nothing to do with
   * the codec, and blaming those would transcode a film because a disk was
   * briefly busy.
   *
   * Only direct play is watched. A transcode already gave up the claim, and a
   * conversion that stutters is a different problem with a different fix.
   */
  const decodeBase = useRef<Sample | null>(null);
  const poorDecode = useRef(false);

  useEffect(() => {
    decodeBase.current = null;
    poorDecode.current = false;
  }, [itemID]);

  useEffect(() => {
    if (transcoding.current || !playing) {
      decodeBase.current = null;
      return;
    }
    const id = window.setInterval(() => {
      const v = videoRef.current;
      if (!v || v.paused || poorDecode.current) return;
      // HAVE_FUTURE_DATA or better. Below that the element is starved, and
      // frames missed while starving say nothing about the decoder.
      if (v.readyState < 3) {
        decodeBase.current = null;
        return;
      }
      const q = v.getVideoPlaybackQuality?.();
      if (!q) return; // Engines without the counters simply opt out.
      const now: Sample = {
        at: Date.now(),
        decoded: q.totalVideoFrames,
        dropped: q.droppedVideoFrames,
      };
      if (!decodeBase.current) {
        decodeBase.current = now;
        return;
      }
      if (struggling(decodeBase.current, now)) {
        poorDecode.current = true;
        fallBackFromPoorDecode();
      }
    }, 2000);
    return () => window.clearInterval(id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [itemID, playing, audioIndex]);

  /*
   * Withdraw the claim that made this direct-play, and convert instead.
   *
   * Deliberately the same withdrawal `retryWithoutClaims` performs, for the
   * same reason it is narrow: only the capabilities *this file needed* are at
   * risk, because a file cannot be ruined by a claim it never used. One
   * unplayable file once took seven claims down with it and the server
   * re-encoded everything for a fortnight.
   *
   * Resumes where the viewer is, not where they started. This fires in the
   * middle of a film — sending them back to the beginning to fix the picture
   * would be a worse bug than the one being fixed.
   */
  const fallBackFromPoorDecode = useCallback(() => {
    const v = videoRef.current;
    if (!v || !item || transcoding.current) return;

    const claimed = capabilities();
    if (claimed) {
      const atRisk = capabilitiesNeededBy(item?.streams);
      const claims = claimed.split(",");
      let news = false;
      for (const c of atRisk.filter((c) => claims.includes(c))) {
        if (deny(c)) news = true;
      }
      if (news) resetCapabilities();
    }

    const at = v.currentTime;
    setNote("That file was dropping frames — converting it instead");
    decision.current = {
      method: "transcode",
      reason: "direct playback dropped too many frames",
    };
    transcoding.current = true;
    offset.current = at;
    startedFrom.current = at;
    setSubOffset(at);
    setCurrent(0);
    setLoading(true);
    chosenPath.current = filePath(
      "transcode",
      hlsWorthTrying(mediaCapability().canPlayType),
    );
    v.src = sourceURL(
      item.id,
      "transcode",
      at,
      audioIndex,
      qualityRef.current,
      chosenPath.current,
    );
    v.load();
    void v.play().catch(() => {});
  }, [item, audioIndex]);

  /*
   * A conversion that never started, said out loud.
   *
   * `retryWithoutClaims` deliberately does nothing here — retrying a failed
   * transcode under a narrower profile is the same request again, which is how
   * a failing file becomes an infinite loop. Correct, and it left the other
   * half undone: the element errored, nothing handled it, and the spinner sat
   * there with "Converting…" under it for ever.
   *
   * That is the worst outcome available, because it is indistinguishable from
   * working slowly. A film sat like that while the server had already refused
   * the request — and the refusal was not logged either, so afterwards it could
   * not be told apart from a request that never arrived.
   *
   * No probe of the URL to find out why. Asking again would start the very
   * transcode that was refused, taking a slot to explain why there were no
   * slots. The reason is in the server log; what belongs here is that it
   * failed, and that waiting will not fix it.
   */
  const transcodeFailed = useCallback(() => {
    setLoading(false);
    setNote(
      "The conversion did not start. The server may already be converting as " +
        "much as it can — check Activity in Settings, or try again shortly.",
    );
  }, []);

  // ---- seeking --------------------------------------------------------------
  const seekTo = useCallback(
    (target: number) => {
      const v = videoRef.current;
      if (!v) return;
      // Seeking is somebody at the controls, so it ends the run.
      noteAttention();
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
        chosenPath.current = filePath(
          decision.current.method,
          hlsWorthTrying(mediaCapability().canPlayType),
        );
        v.src = sourceURL(
          itemID,
          decision.current.method,
          t,
          audioIndex,
          qualityRef.current,
          chosenPath.current,
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
    const host = (
      window as { lancastToggleFullscreen?: () => Promise<boolean> }
    ).lancastToggleFullscreen;
    if (host) {
      // The binding is a toggle, so this asks only when there is something to
      // leave — calling it blind would put a windowed player into fullscreen,
      // which is the opposite of what Escape means.
      if (fullscreen) void host().then((on) => setFullscreen(Boolean(on)));
      return;
    }
    if (document.fullscreenElement)
      void document.exitFullscreen().catch(() => {});
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
    const host = (
      window as { lancastToggleFullscreen?: () => Promise<boolean> }
    ).lancastToggleFullscreen;
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
    const v = videoRef.current as
      | (HTMLVideoElement & {
          setSinkId?: (id: string) => Promise<void>;
        })
      | null;
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

  /*
   * The pop-out window.
   *
   * `popoutWin` is state so the portal re-renders when the window opens or
   * closes; the element move itself is imperative and happens in the effect
   * below, outside React's reconciliation. That split is the whole design: React
   * owns the controls in the other document, and the media element is moved by
   * hand because a portal would unmount it and unmounting stops the sound.
   */
  const [popoutWin, setPopoutWin] = useState<Window | null>(null);
  const [popoutRoot, setPopoutRoot] = useState<HTMLElement | null>(null);
  /*
   * Feature detection answers "does this host implement Document PiP". It does
   * not answer "will it give me a window", and those come apart: an embedded
   * WebView reports the API and then fails the call with InvalidStateError,
   * because there is no window manager behind it.
   *
   * There is no way to find that out without asking, and asking requires a user
   * gesture — so the first click is the probe. When it fails, this remembers,
   * and every later click takes the browser picture-in-picture path instead.
   * That path has to be reached *synchronously* from the click: the failed
   * attempt consumes the transient activation, so a fallback inside the catch
   * is already too late to open anything.
   */
  const [popoutRefused, setPopoutRefused] = useState(false);
  const popoutAvailable = popoutSupported() && !popoutRefused;

  const closePopout = useCallback(() => {
    setPopoutWin((w) => {
      w?.close();
      return null;
    });
    setPopoutRoot(null);
  }, []);

  const togglePopout = useCallback(() => {
    if (popoutWin) {
      closePopout();
      return;
    }
    const v = videoRef.current;
    const aspect =
      v && v.videoWidth > 0 && v.videoHeight > 0
        ? v.videoWidth / v.videoHeight
        : undefined;
    void openPopout(aspect)
      .then((opened) => {
        if (!opened) return;
        // The browser's own close button, and the "return to tab" affordance,
        // both fire pagehide on the window rather than telling us directly.
        opened.win.addEventListener("pagehide", () => {
          setPopoutWin(null);
          setPopoutRoot(null);
        });
        setPopoutWin(opened.win);
        setPopoutRoot(opened.root);
      })
      .catch(() => {
        /*
         * The window could not be opened. Remember it, so the button stops
         * offering something this host cannot do and becomes the browser
         * picture-in-picture control instead — which is a worse pop-out, and
         * the one the ADR keeps as the fallback for exactly this.
         *
         * Found by clicking the button rather than by reasoning about it.
         * Before this, the rejection was unhandled and the button did nothing
         * at all, forever: the dead control the ADR says not to ship.
         */
        setPopoutRefused(true);
        // Attempted anyway, because in a host that merely declined this once it
        // may still work — and if the activation has already been spent, this
        // fails silently and the next click takes the synchronous path above.
        const v = videoRef.current;
        if (v && !isAudio && document.pictureInPictureEnabled) {
          v.requestPictureInPicture().catch(() => {});
        }
      });
  }, [popoutWin, closePopout]);

  /*
   * Move the element out, and bring it home.
   *
   * The stage is found by attribute rather than by ref, because the node lives
   * in a portal rendered into the other document and a ref would arrive on a
   * different commit than this effect. The element is returned to its slot on
   * cleanup — including when the window is closed by the browser's own button,
   * which is why the effect depends on the window rather than on a flag.
   */
  useEffect(() => {
    const el = videoRef.current;
    if (!popoutRoot || !el) return;

    const slot = el.parentElement;
    const stage = popoutRoot.querySelector<HTMLElement>("[data-popout-stage]");
    if (!stage || !slot) return;

    moveElement(el, stage);
    return () => {
      // Home is the slot it came from. If the provider itself has gone, there
      // is nothing to return to and nothing playing to protect.
      if (slot.isConnected) moveElement(el, slot);
    };
  }, [popoutRoot]);

  // A window left open after playback stops is a floating black rectangle.
  useEffect(() => {
    if (popoutWin && surface === "idle") closePopout();
  }, [popoutWin, surface, closePopout]);

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
    stillWatching,
    keepWatching,
    playNext,
    upNext,
    playNextUp,
    addToQueue,
    removeFromUpNext,
    playPrev,
    playFromQueue,
    prefs,
    setPrefs: updatePrefs,
    videoRef,
    containerRef,
    claimFullSurface,
    popoutAvailable,
    popout: popoutWin !== null,
    togglePopout,
  };

  return (
    <Ctx.Provider value={value}>
      {children}
      {/* Our controls, in the other document. A portal keeps React context —
          the playback state, the router, the query client — so these are the
          same components behaving the same way, in a different window. */}
      {popoutRoot &&
        createPortal(<PopoutPlayer onClose={closePopout} />, popoutRoot)}
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
              // Frames from a playlist are the only proof that this engine can
              // read one. Recorded so the question is not re-opened on every
              // film, and so a later decode error cannot be mistaken for the
              // engine lacking HLS altogether.
              if (chosenPath.current === "hls") rememberHLS("playable");
            }}
            onWaiting={() => setLoading(true)}
            onError={(e) => {
              /*
               * A playlist this engine cannot read, which is not a failure of
               * the film or the server.
               *
               * `canPlayType` answers "maybe" to a playlist on Chromium whether
               * or not it will play one, so the capability cannot be asked for
               * up front — it is discovered here, once, and remembered for the
               * device (fileTransport.ts). Falling straight back to the
               * progressive source means the cost of finding out is a reload
               * rather than a dead player.
               *
               * Narrow deliberately: only *unsupported source* counts. A decode
               * or network error says something about this file or this moment,
               * and retiring the better path over one of those would be a
               * permanent decision made from a transient fault.
               */
              if (
                chosenPath.current === "hls" &&
                isUnsupportedSource(e.currentTarget.error)
              ) {
                rememberHLS("refused");
                const v = e.currentTarget;
                chosenPath.current = "progressive";
                setLoading(true);
                v.src = sourceURL(
                  itemID,
                  decision.current.method,
                  offset.current,
                  audioIndex,
                  qualityRef.current,
                  "progressive",
                );
                v.load();
                void v.play().catch(() => {});
                return;
              }
              // Two different failures wearing one event. A direct play that
              // fails is a claim to withdraw; a transcode that fails is the
              // server saying no, and only one of them is worth retrying.
              if (transcoding.current) transcodeFailed();
              else retryWithoutClaims();
            }}
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
              // Pausing is a person. Not the pause this feature performs
              // itself, which happens after the prompt is already set and
              // would otherwise clear it on the way past.
              if (!stillWatchingRef.current) noteAttention();
            }}
            onEnded={() => {
              saveProgress(true);
              /*
               * A stream that was cut is not a film that ended.
               *
               * A progressive fMP4 has no duration in it — the element learns
               * how long the film is only by reaching the end of the bytes. So
               * when the server stops sending, for any reason, the browser
               * fires `ended`, exactly as it would at the real end. This handler
               * believed it and rolled on to the next title.
               *
               * The reason it stops is the idle reaper. Pausing a progressive
               * stream applies backpressure, the session stops being read, and
               * nothing distinguishes "paused" from "gone" — so a film paused
               * longer than the idle timeout had its ffmpeg killed underneath
               * it, and pressing play skipped to the next film. It only ever
               * happened to convertible codecs, because direct play serves a
               * real file whose duration is known up front.
               *
               * The probed runtime is the authority on where the end actually
               * is, so short of it means cut. seekTo re-requests the transcode
               * from there, which is the same thing a seek already does.
               */
              if (
                transcoding.current &&
                totalDuration > 0 &&
                displayTime < totalDuration - TRUNCATION_SLACK
              ) {
                const at = displayTime;
                const looping =
                  recoveredAt.current !== null &&
                  Math.abs(recoveredAt.current - at) < TRUNCATION_SLACK;
                if (!looping) {
                  recoveredAt.current = at;
                  setNote("Reconnecting…");
                  setLoading(true);
                  seekTo(at);
                  return;
                }
                // Cut twice at the same spot. Recovering again would reload for
                // ever, so fall through and treat it as the end.
              }
              recoveredAt.current = null;
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
              /*
               * The queue moving on by itself is the thing being counted.
               *
               * Asked *before* advancing rather than after, because the point
               * is to stop the next transcode from starting and the next
               * progress record from being written — a prompt that appears
               * over an episode already playing has let the thing happen that
               * it exists to prevent.
               */
              const now = Date.now();
              const next = advanced(runRef.current, now);
              runRef.current = next;
              if (shouldAsk(next)) {
                stillWatchingRef.current = describeRun(next, now);
                setStillWatching(stillWatchingRef.current);
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
