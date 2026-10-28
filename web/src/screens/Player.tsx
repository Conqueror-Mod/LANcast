import { typingInto } from "@/lib/typing";
import { WINDOW_HIDDEN } from "@/components/TrayBridge";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  useLocation,
  useNavigate,
  useParams,
  useSearchParams,
} from "react-router-dom";
import { useBackHandler, useSuspendFocus } from "@/focus/FocusController";
import { shuffleForEntry } from "@/playback/queueOrder";
import { clock } from "@/lib/format";
import { showsSubtitleButton } from "@/lib/subtitleButton";
import { matchesBinding, bindingKeys } from "@/lib/keys";
import { Scrubber } from "@/components/Scrubber";
import { NowPlaying, VizStep, stepVisualizer } from "@/components/NowPlaying";
import { useMilkdropOffer } from "@/playback/milkdropStage";
import { stageTier } from "@/playback/prefs";
import { elementFXSupported } from "@/playback/elementEngine";
import { PlaybackStats } from "@/components/PlaybackStats";
import { PlaybackSettings } from "@/components/PlaybackSettings";
import { QueuePanel } from "@/components/QueuePanel";
import { TogetherPanel } from "@/components/TogetherPanel";
import { AddToPlaylist } from "@/components/AddToPlaylist";
import { SkipGlyph } from "@/components/SkipGlyph";
import {
  ShuffleGlyph,
  RepeatGlyph,
  VolumeGlyph,
  SettingsGlyph,
  QueueGlyph,
  TogetherGlyph,
  NightGlyph,
  DialogueGlyph,
  PipGlyph,
  FullscreenGlyph,
  PrevGlyph,
  NextGlyph,
  StopGlyph,
} from "@/components/PlayerGlyphs";
import { usePlayback, useFullSurface } from "@/playback/PlaybackProvider";
import { AUTO_SKIP_IDLE, autoSkipStep, skipTarget, type AutoSkipState } from "@/lib/skip";
import { fetchPreroll, useChapters, usePlaybackPrefs, usePreviews } from "@/api/hooks";
import { PrerollOverlay } from "@/components/PrerollOverlay";
import { nextChapterStart, previousChapterStart, type ChapterMark } from "@/lib/chapters";
import { UpNextCard } from "@/components/UpNextCard";
import { AfterFilm } from "@/components/AfterFilm";
import { DEFAULTS, DIALOGUE_LEVELS } from "@/playback/prefs";
import {
  canBoostDialogue,
  canNightFilm,
  canNightMusic,
  nextDialogueLevel,
} from "@/playback/soundControls";
import "./Player.css";

// One empty list, so a film without chapters does not hand the key effect a
// new array on every render.
const NO_CHAPTERS: ChapterMark[] = [];

// The player screen is chrome. The media element and everything that drives it
// live in PlaybackProvider, above the router, so that leaving this screen docks
// the player instead of stopping it (docs/music-client-plan.md).
//
// What remains here is what belongs to *being on the player screen*: the
// transport, the idle auto-hide, the subtitle menu, and the keyboard surface.
// How long "Intro skipped" stays, with its Watch intro button (ADR 0083).
const INTRO_NOTICE_MS = 8000;

export function Player() {
  const { id } = useParams();
  const itemID = Number(id);
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();

  const pb = usePlayback();

  // Hold the full-screen surface while this screen is mounted; releasing it on
  // unmount is what produces the mini-player.
  useFullSurface();

  // The route says what to play. play() is a no-op when it is already playing
  // that item, which is what makes returning from the mini-player continuous
  // rather than a restart.
  //
  /*
   * A queue arrives one of two ways.
   *
   * ?queue= is the original, and stays: it is short, it survives a reload, and
   * it makes an album or a season a linkable thing. It does not scale. "Play
   * all" over a music library is every track in it — a few thousand ids is tens
   * of kilobytes of URL, and history entries are not the place for that.
   *
   * So a large queue is handed over in history state instead, which is held in
   * memory and never parsed by anything. It cannot be linked to, and that is
   * the honest trade: "everything in this library, shuffled" is not a thing
   * anyone bookmarks, where "this album from track 4" is.
   *
   * State wins when both are present. Nothing sends both.
   */
  const handoff = (useLocation().state ?? null) as {
    queue?: number[];
    shuffle?: boolean;
    fromStart?: boolean;
  } | null;
  const stateQueue = handoff?.queue;
  const stateShuffle = handoff?.shuffle;
  const fromStart = !!handoff?.fromStart;

  const queueParam = searchParams.get("queue");
  const { play, setShuffle } = pb;
  /*
   * The session's current flag, read through a ref.
   *
   * shuffleForEntry needs to know what shuffle is *now* in order to leave it
   * alone, but this effect must not re-run when it changes — setShuffle is one
   * of the things the effect calls, so a dependency on the value it writes is
   * a loop.
   */
  const shuffleRef = useRef(pb.shuffle);
  shuffleRef.current = pb.shuffle;
  /*
   * And what is playing, read the same way and for the same reason: this
   * effect must not re-run when the item changes underneath it, because it is
   * the thing that changes it.
   */
  const playingRef = useRef(pb.itemID);
  playingRef.current = pb.itemID;

  /*
   * The pre-roll gate (ADR 0099).
   *
   * A film entered fresh — not one already playing, which is coming back from
   * the mini-player — asks the server first whether a clip plays before it.
   * The play effect below waits for the answer, and for the clip to end; with
   * pre-roll off the answer is immediate and nothing is shown. The server owns
   * the rule (a film, from the start, this account not opted out), so a
   * resume, an episode or a track all come back with no clip.
   */
  const [gate, setGate] = useState<{ id: number; clip: string | null; ready: boolean }>({
    id: 0,
    clip: null,
    ready: false,
  });
  useEffect(() => {
    if (!itemID) return;
    if (playingRef.current === itemID) {
      setGate({ id: itemID, clip: null, ready: true });
      return;
    }
    let live = true;
    setGate({ id: itemID, clip: null, ready: false });
    fetchPreroll(itemID, fromStart)
      .then((a) => {
        if (!live) return;
        const key = a.clip?.key;
        setGate({ id: itemID, clip: key ? `/api/preroll/${encodeURIComponent(key)}` : null, ready: !key });
      })
      // A question that fails is a film that starts: pre-roll is never worth
      // keeping somebody from what they pressed Play on.
      .catch(() => {
        if (live) setGate({ id: itemID, clip: null, ready: true });
      });
    return () => {
      live = false;
    };
  }, [itemID, fromStart]);
  const gateOpen = gate.id === itemID && gate.ready;

  useEffect(() => {
    if (!itemID || !gateOpen) return;
    // Both forms of "here is the queue" count: history state, and the ?queue=
    // the track list and the collection page still use.
    const suppliedQueue = (stateQueue && stateQueue.length > 0) || !!queueParam;
    const queue =
      stateQueue && stateQueue.length > 0
        ? stateQueue
        : queueParam
          ? queueParam.split(",").map(Number)
          : [itemID];
    /*
     * "Play from start" (the detail page of a title part watched). For one
     * already playing, a seek to the top; otherwise the player is told to
     * ignore the saved position for this one item, once.
     */
    if (fromStart && playingRef.current === itemID) {
      pb.seekTo(0);
    }
    play(itemID, queue, fromStart);
    /*
     * An explicit request wins; supplying a queue means "in this order";
     * supplying nothing leaves the session's flag alone.
     *
     * This used to be "only when the caller said so", full stop, on the
     * reasoning that starting an album should not silently clear a shuffle you
     * had turned on. That is right for an entry carrying no queue — coming
     * back from the mini-player — and wrong for one that hands over a
     * sequence, which is a statement about order that four callers were making
     * without knowing it. See shuffleForEntry.
     */
    setShuffle(
      shuffleForEntry(
        stateShuffle,
        suppliedQueue,
        shuffleRef.current,
        playingRef.current === itemID,
      ),
    );
    // stateQueue is stable for a history entry; its length identifies it well
    // enough to avoid re-running on an unrelated render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [itemID, queueParam, play, setShuffle, stateQueue?.length, stateShuffle, gateOpen]);

  // **The URL is not kept in step with the queue, deliberately.**
  //
  // The first version of this screen wrote the advancing id back into the route,
  // so the address would follow the record. It could not tell that apart from a
  // *navigation*: clicking a second film set the route to the new id while the
  // provider still held the old one, and the sync dragged the route back — the
  // previous film played instead of the one that was clicked, and every bounce
  // started another transcode. That is the lag and the wrong-film bug.
  //
  // Nothing needs the URL to move. This screen renders from the provider, so it
  // already shows the right track; Back goes to the container because history
  // was never touched; and the docked player expands to whatever is actually
  // playing. The one cost is that reloading the page mid-queue restarts at the
  // track the address still names, which is a fair price for not having two
  // things race to own what is playing.

  const [chromeVisible, setChromeVisible] = useState(true);
  /*
   * The statistics overlay, off unless asked for.
   *
   * A diagnostic rather than a feature: it answers "is the picture dropping
   * frames" in a glance, which is the question that cost an evening of GPU
   * counters and ffprobe when a film played badly and the player could say
   * nothing about it. Permanently visible it would be clutter on every film to
   * serve the rare one that misbehaves.
   */
  const [showStats, setShowStats] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  /*
   * Whether anything behind the settings button is away from its default.
   *
   * The five separate buttons this replaced each lit up on their own, so the
   * strip said at a glance that the speed was up or a non-default audio track
   * was selected. Collapsing them into one panel would have thrown that signal
   * away — and it is the signal that answers "why does this sound wrong" a week
   * later, when the setting has long been forgotten.
   *
   * Deliberately not gold: an engaged control reads through weight and a filled
   * background. Gold is where you are, and nothing else.
   */
  const settingsChanged =
    pb.speed !== 1 ||
    pb.prefs.quality !== DEFAULTS.quality ||
    pb.prefs.audioDevice !== DEFAULTS.audioDevice ||
    pb.prefs.autoPlay !== DEFAULTS.autoPlay ||
    pb.prefs.subOffset !== DEFAULTS.subOffset ||
    // The audio track is engaged when it is not the one the file leads with,
    // which is the stream flagged `default` rather than simply the first — a
    // release with a commentary track first is unusual but entirely legal.
    (pb.audioIndex != null &&
      pb.audioIndex !==
        (pb.audioTracks.find((t) => t.default) ?? pb.audioTracks[0])?.index);
  const [queueOpen, setQueueOpen] = useState(false);
  const [togetherOpen, setTogetherOpen] = useState(false);
  /*
   * The phone's overflow (docs/design.md, Phone). Fourteen controls do not fit
   * 375px, and seven of them sat off the right edge where nothing could reach
   * them. On a phone the options group is hidden until "⋯" opens it as a panel
   * above the bar; on a desktop the button is not drawn and the group is
   * always laid out, so this state means nothing there.
   */
  const [moreOpen, setMoreOpen] = useState(false);
  const [addOpen, setAddOpen] = useState(false);
  const [pipAvailable, setPipAvailable] = useState(false);

  // Picture-in-picture is offered only where it exists. WebView2 is a different
  // host from the browser and may not implement it, and a button that does
  // nothing is worse than one that is absent — it reads as a broken feature
  // rather than an unavailable one.
  useEffect(() => {
    setPipAvailable(
      typeof document !== "undefined" && !!document.pictureInPictureEnabled,
    );
  }, []);

  // Leaving the player leaves it playing. That is the point of the change, and
  // it is why Back no longer stops anything.
  const close = useCallback(() => navigate(-1), [navigate]);
  // Stop is close plus teardown. pb.stop() already saves progress, drops the
  // source (which ends the transcode) and clears the queue, so nothing
  // auto-advances behind you and the mini-player has nothing to dock — the same
  // verb the mini-player's stop performs, so it means one thing in both places.
  const stopAndLeave = useCallback(() => {
    pb.stop();
    navigate(-1);
  }, [pb, navigate]);
  /*
   * ✕ during a film: the player stops it (PlaybackProvider), and this screen
   * leaves with it, as Stop does. Reported after the close fix: reopening
   * from the tray came back to this screen with nothing behind it — black,
   * 0:00 / 0:00 — because the film had stopped and the route had not moved.
   * Music keeps playing with the window away, so its screen stays.
   */
  const leavingRef = useRef({ audio: pb.isAudio, navigate });
  leavingRef.current = { audio: pb.isAudio, navigate };
  useEffect(() => {
    const onHidden = () => {
      if (!leavingRef.current.audio) leavingRef.current.navigate(-1);
    };
    window.addEventListener(WINDOW_HIDDEN, onHidden);
    return () => window.removeEventListener(WINDOW_HIDDEN, onHidden);
  }, []);
  useSuspendFocus();
  /*
   * Escape is Back, and Back leaves fullscreen before it leaves the player.
   *
   * Wired through the back stack rather than as a second Escape listener,
   * because this app resolves Escape centrally on purpose — the controller
   * binds it on `document` and the player's own keydown is on `window`, which
   * fires later, so a case for it here could never win. It did not: pressing
   * Escape in fullscreen closed the player and left a borderless window over
   * the library, with the film playing on in the corner.
   *
   * Fullscreen first, close second. Closing from fullscreen would leave the
   * window borderless over a page that has no idea it is, which is a stranger
   * place to end up than where you started.
   */
  /*
   * The full-screen visualizer comes off before either: it is a layer of this
   * screen, and Escape from it returns to Now Playing, not out of the player
   * (docs/now-playing-plan.md). On a TV the window is fullscreen for good, and
   * Escape leaving that instead would strand the field over a desktop.
   */
  const back = useCallback(() => {
    if (pb.vizFull) {
      pb.setVizFull(false);
      return;
    }
    if (pb.fullscreen) {
      pb.exitFullscreen();
      return;
    }
    close();
  }, [pb, close]);
  useBackHandler(back);

  /*
   * And any other way out ends fullscreen too.
   *
   * Escape is handled above, but the player's own close and dock buttons, a
   * mouse back button and anything else that navigates away unmount this
   * screen without asking. In the desktop window fullscreen is the *window*,
   * borderless and the size of the monitor, so leaving the player that way
   * left Home under a window with no title bar, no minimize, maximize or close
   * — and no fullscreen control, because only this screen has one. Reported
   * as having to start another film just to get out.
   */
  const fullscreenNow = useRef(pb.fullscreen);
  fullscreenNow.current = pb.fullscreen;
  const exitFullscreenNow = useRef(pb.exitFullscreen);
  exitFullscreenNow.current = pb.exitFullscreen;
  useEffect(
    () => () => {
      if (fullscreenNow.current) exitFullscreenNow.current();
    },
    [],
  );

  // ---- auto-hide chrome -----------------------------------------------------
  const idleTimer = useRef<number>();
  // Set by a click on the picture, cancelled by a second one. See the handlers.
  const clickTimer = useRef<number>();
  /*
   * Whether the pointer is resting on the controls.
   *
   * A ref rather than state: it is read inside a timeout that must not be
   * rebuilt every time the pointer crosses the bar, and nothing renders from
   * it.
   */
  const overChrome = useRef(false);
  const wakeChrome = useCallback(() => {
    setChromeVisible(true);
    window.clearTimeout(idleTimer.current);
    // Audio keeps its controls. Chrome hides to get out of the way of the
    // picture; over a still cover there is nothing to get out of the way of,
    // and hiding the transport would leave a motionless screen that looks
    // frozen rather than playing. The full-screen visualizer is the one
    // exception: it is a picture, and fades its chrome exactly as a film does.
    if (pb.isAudio && !pb.vizFull) return;
    idleTimer.current = window.setTimeout(function tick() {
      /*
       * The controls do not vanish from under the pointer.
       *
       * Waking was driven entirely by `mousemove`, and clicking the same spot
       * repeatedly — next, next, next — produces no movement at all. So the
       * timer expired with the pointer sitting on the button somebody was
       * using, and the bar faded out beneath it. Reported after three clicks,
       * which is exactly how long 2.5 seconds of clicking takes.
       *
       * Re-armed rather than cancelled, so leaving the bar without moving
       * anywhere else still hides it a moment later.
       */
      if (overChrome.current) {
        idleTimer.current = window.setTimeout(tick, 2500);
        return;
      }
      if (pb.playing) setChromeVisible(false);
    }, 2500);
  }, [pb.isAudio, pb.playing, pb.vizFull]);

  // Into the full-screen visualizer, the idle clock starts; out of it, the
  // chrome is back for good, as it always is over a track.
  const wasVizFull = useRef(pb.vizFull);
  useEffect(() => {
    if (wasVizFull.current === pb.vizFull) return;
    wasVizFull.current = pb.vizFull;
    wakeChrome();
  }, [pb.vizFull, wakeChrome]);

  /*
   * The prompt has to be visible when it appears, which it was not.
   *
   * `.player__chrome` is `opacity: 0` while the player is idle, and the idle
   * timer only *hides* when playing — nothing shows it again on pause. So by
   * the time "are you still watching" fires, after hours nobody has touched,
   * the chrome has long since faded and the prompt rendered into it invisibly.
   *
   * That is the exact failure this feature exists to avoid: playback stopped,
   * and the screen said nothing about why. Waking the chrome is enough,
   * because the hide timer checks `pb.playing` and playback is already paused.
   */
  useEffect(() => {
    if (pb.stillWatching) wakeChrome();
  }, [pb.stillWatching, wakeChrome]);

  useEffect(
    () => () => {
      window.clearTimeout(idleTimer.current);
      window.clearTimeout(clickTimer.current);
    },
    [],
  );

  // ---- keyboard (transport surface owns its keys; spatial nav is suspended) --
  const {
    togglePlay,
    toggleFullscreen,
    toggleMute,
    seekBy,
    cycleSub,
    changeVolume,
    setVizFull,
  } = pb;
  // A visualizer can be on screen: a track, with Web Audio to tap.
  const vizOffered = pb.isAudio && elementFXSupported();
  const vizShowing = vizOffered && (pb.vizFull || stageTier(pb.prefs.stage) > 0);
  // The stored MilkDrop presets, each a stop for `,` and `.` (ADR 0091).
  const milkdropIDs = useMilkdropOffer(vizOffered).ids;
  // The file's chapters, for the scrub bar and Page Up / Page Down. A track
  // has none worth asking for; a film or an episode often does.
  const chapters = useChapters(pb.item?.id ?? 0, !pb.isAudio).data?.chapters ?? NO_CHAPTERS;
  // The file's scrub-bar previews (ADR 0105), once the background pass has
  // made them. Not for a track: a song has no pictures to preview.
  const previewItem = pb.item?.id ?? 0;
  const previewSheets = usePreviews(previewItem, !pb.isAudio).data;
  const previews = useMemo(
    () =>
      previewSheets
        ? { sheets: previewSheets, url: (n: number) => `/api/items/${previewItem}/previews/${n}` }
        : null,
    [previewSheets, previewItem],
  );
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // While typing in the subtitle search box, keys are text — not transport.
      const ae = document.activeElement;
      if (typingInto(ae)) {
        return;
      }
      /*
       * Matched against the bindings rather than against literals.
       *
       * These were a switch on hard-coded keys, which meant the keyboard
       * customizer could store an override, render it back, and change
       * nothing — the pane and the player disagreeing about what a key does
       * is the exact failure the single key map exists to prevent.
       *
       * Seek and volume stay on the arrows because those bindings are fixed:
       * they are how you move through a film and around a grid, and the
       * customizer refuses to rebind them for the same reason.
       */
      if (matchesBinding("playpause", e.key)) {
        e.preventDefault();
        togglePlay();
      } else if (matchesBinding("fullscreen", e.key)) {
        toggleFullscreen();
      } else if (matchesBinding("mute", e.key)) {
        toggleMute();
      } else if (matchesBinding("vizfull", e.key)) {
        if (vizOffered) setVizFull(!pb.vizFull);
      } else if (e.key === "ArrowLeft") {
        e.preventDefault();
        seekBy(-10);
      } else if (e.key === "ArrowRight") {
        e.preventDefault();
        seekBy(10);
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        changeVolume((pb.muted ? 0 : pb.volume) + 0.05);
      } else if (e.key === "ArrowDown") {
        e.preventDefault();
        changeVolume((pb.muted ? 0 : pb.volume) - 0.05);
      } else {
        // The subtitle pair is one binding with two keys: the first cycles
        // back, the second forward, whatever they have been rebound to.
        const subs = bindingKeys("subtitles");
        const viz = bindingKeys("vizpreset");
        const chap = bindingKeys("chapter");
        if (e.key === chap[0] || e.key === chap[1]) {
          // Only when the file has chapters; otherwise the keys do nothing
          // rather than something surprising.
          const to =
            e.key === chap[0]
              ? previousChapterStart(chapters, pb.displayTime)
              : nextChapterStart(chapters, pb.displayTime);
          if (to !== undefined) {
            e.preventDefault();
            pb.seekTo(to);
          }
        } else if (e.key === subs[0]) cycleSub(-1);
        else if (e.key === subs[1]) cycleSub(1);
        // Only while a visualizer is showing: over the cover they would turn
        // one on, and routing the music is not a side effect a stray key gets.
        else if (e.key === viz[0]) {
          if (vizShowing) stepVisualizer(-1, undefined, milkdropIDs);
        } else if (e.key === viz[1]) {
          if (vizShowing) stepVisualizer(1, undefined, milkdropIDs);
        }
        else if (e.key === "i" || e.key === "I") {
          /*
           * `i`, for information: the statistics overlay.
           *
           * Fixed rather than rebindable, and checked *last* on purpose. It is
           * a diagnostic somebody reaches for once when something looks wrong,
           * so it has not earned a place in the keyboard customizer beside the
           * transport keys used every day — but a fixed key that ran before the
           * bindings would silently shadow anything rebound to `i`, and the
           * player disagreeing with the customizer about what a key does is the
           * exact failure the single key map exists to prevent.
           */
          setShowStats((v) => !v);
        }
      }
      wakeChrome();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [
    togglePlay,
    toggleFullscreen,
    toggleMute,
    seekBy,
    cycleSub,
    changeVolume,
    setVizFull,
    pb.muted,
    pb.volume,
    pb.vizFull,
    vizOffered,
    vizShowing,
    milkdropIDs,
    chapters,
    pb.seekTo,
    pb.displayTime,
    wakeChrome,
  ]);

  const item = pb.item;

  /*
   * The skip on offer right now, or nothing.
   *
   * Recomputed from the playhead rather than held in state: it is a pure
   * function of where we are, and storing it would mean a second thing that can
   * disagree with the first — a button still showing after a seek past the
   * intro, which is the exact bug this shape cannot have.
   */
  const skip = skipTarget(
    item?.markers,
    pb.displayTime,
    pb.totalDuration,
    item?.kind,
  );

  /*
   * Up Next takes the place of Skip credits on an episode with something after
   * it. Both appear at the same moment and offer the same way out of the
   * credits, and two controls doing one job in the corner of a picture is one
   * too many. Cancelling it hands the corner back to Skip credits, for this
   * episode only — the next one asks again.
   */
  const [upNextCancelled, setUpNextCancelled] = useState<number | null>(null);

  /*
   * The intro skipping itself (ADR 0083). The rule is autoSkipStep, stepped on
   * every position update; this only performs what it decides and shows what
   * it did. Off until the account's answer arrives, so a slow fetch can cost
   * one skip but never cause one somebody turned off.
   */
  const skipIntros = usePlaybackPrefs().data?.skip_intros === true;
  const autoSkip = useRef<AutoSkipState>(AUTO_SKIP_IDLE);
  const [introSkipped, setIntroSkipped] = useState<{ itemID: number | null; from: number } | null>(
    null,
  );
  const { seekTo } = pb;
  useEffect(() => {
    const r = autoSkipStep(autoSkip.current, {
      itemID: pb.itemID,
      enabled: skipIntros,
      playing: pb.playing,
      markers: item?.markers,
      positionSeconds: pb.displayTime,
      durationSeconds: pb.totalDuration,
      itemKind: item?.kind,
    });
    autoSkip.current = r.state;
    if (r.skip) {
      seekTo(r.skip.toSeconds);
      setIntroSkipped({ itemID: pb.itemID, from: r.skip.fromSeconds });
    }
  }, [pb.itemID, pb.playing, pb.displayTime, pb.totalDuration, item?.markers, item?.kind, skipIntros, seekTo]);
  // The notice stays long enough to read and press, then hands the corner back.
  useEffect(() => {
    if (!introSkipped) return;
    const t = setTimeout(() => setIntroSkipped(null), INTRO_NOTICE_MS);
    return () => clearTimeout(t);
  }, [introSkipped]);
  const showIntroSkipped = introSkipped != null && introSkipped.itemID === pb.itemID;
  const showUpNext =
    skip?.kind === "credits" &&
    item?.kind === "episode" &&
    pb.nextItemID != null &&
    upNextCancelled !== pb.itemID;
  /*
   * A film that played to its end with nothing queued after it ends on
   * suggestions (AfterFilm). Not while anything plays — Watch again, or a
   * press of play, takes it away — and never for an episode or a track, whose
   * endings are Up Next's and Now Playing's.
   */
  const showAfter =
    pb.endedID != null &&
    pb.endedID === pb.itemID &&
    !pb.playing &&
    !pb.isAudio &&
    item?.kind === "movie";
  const { rollOn } = pb;
  const rollOnUnattended = useCallback(() => rollOn(false), [rollOn]);

  return (
    <div
      className={
        "player" +
        (chromeVisible ? "" : " player--idle") +
        (pb.isAudio ? " player--audio" : "") +
        (pb.vizFull ? " player--vizfull" : "")
      }
      onMouseMove={wakeChrome}
      onClick={(e) => {
        // Clicks land on this overlay, not the element underneath it, so the
        // click-to-pause target is the empty area rather than the video itself.
        if (e.target !== e.currentTarget) return;
        /*
         * Held for a moment, in case a second click is coming.
         *
         * A double-click fires *two* click events and then dblclick, so play
         * was toggled twice — a no-op — and the compensating toggle written to
         * cancel "the" click made it an odd number again: double-clicking out
         * of fullscreen paused the film. Counting clicks is the wrong tool.
         * Waiting is the right one, and it is what every video player does.
         */
        window.clearTimeout(clickTimer.current);
        clickTimer.current = window.setTimeout(() => pb.togglePlay(), 220);
      }}
      onDoubleClick={(e) => {
        // Fullscreen, and the pending play toggle is cancelled rather than
        // undone: a double-click is one gesture and must not also change what
        // the film is doing.
        if (e.target !== e.currentTarget || pb.isAudio) return;
        window.clearTimeout(clickTimer.current);
        pb.toggleFullscreen();
      }}
    >
      {gate.id === itemID && gate.clip && !gate.ready && (
        <PrerollOverlay
          src={gate.clip}
          onDone={() => setGate((g) => ({ ...g, clip: null, ready: true }))}
        />
      )}
      {/* A black rectangle with a running clock is what a failed playback looks
          like too. A transcode takes seconds to produce its first frame, and on
          a resumed film the clock starts at the resume point, so without this
          the wait reads as "broken" rather than "starting". The decision's own
          reason is the most useful thing to show while waiting — it says why
          this file is taking longer than the last one. */}
      {pb.loading && (
        <div className="player__loading">
          <span className="player__loading-mark" aria-hidden="true" />
          <span>{pb.note || "Starting…"}</span>
        </div>
      )}

      {showAfter && item && (
        <AfterFilm
          film={item}
          // Replacing, so Back from the next film does not return to the end
          // of this one.
          onPlay={(next) => navigate(`/watch/${next}`, { replace: true })}
          onAgain={() => {
            pb.seekTo(0);
            if (!pb.playing) pb.togglePlay();
          }}
          // Stop, not just leave: the film is over, and leaving alone docked
          // it in the mini player with nothing left to play — seen on the
          // installed build the first time this page was closed.
          onClose={stopAndLeave}
        />
      )}

      {showStats && (
        <PlaybackStats
          video={pb.videoRef.current}
          onClose={() => setShowStats(false)}
        />
      )}

      <div
        className="player__chrome"
        /*
         * Hover is tracked on the chrome rather than on each control, because
         * the gap between two buttons is still the toolbar as far as anybody
         * using it is concerned.
         */
        onMouseEnter={() => {
          overChrome.current = true;
        }}
        onMouseLeave={() => {
          overChrome.current = false;
        }}
      >
        <div className="player__top">
          {/* Over the full-screen visualizer, back is to Now Playing, as
              Escape is; close is a step further. */}
          {pb.vizFull ? (
            <button className="player__icon" onClick={() => setVizFull(false)} aria-label="Back to Now Playing">
              ←
            </button>
          ) : (
            <button className="player__icon" onClick={close} aria-label="Close">
              ←
            </button>
          )}
          {pb.vizFull && <VizStep />}
          {/* A track is named on the page, in Now Playing's column, and
              naming it here as well would say it twice in 400px. */}
          {!pb.isAudio && <span className="player__title">{item?.title}</span>}
          {/* While loading, the same note is the centre of the screen
              (player__loading above); saying it twice crowded the title off a
              phone. Once playing, this is the only place it appears, and it
              still says why this file took the route it did. */}
          {pb.note && !pb.loading && (
            <span className="player__note">{pb.note}</span>
          )}
          {/*
            The prompt states what happened and offers the one action that
            answers it. Not a modal: the film is already stopped, so there is
            nothing to block, and a dialog over a paused player is a second
            thing to dismiss rather than the first thing to read.
          */}
          {pb.stillWatching && (
            <div className="player__still" role="status">
              <span>{pb.stillWatching}</span>
              <button className="set-btn" onClick={pb.keepWatching}>
                Keep watching
              </button>
            </div>
          )}
        </div>

        {/* A track's screen is Now Playing: what is on, and what comes next,
            beside the cover (docs/now-playing-plan.md). */}
        {pb.isAudio && !pb.vizFull && <NowPlaying />}

        {/*
          Skip intro and skip credits. Credits are a button that appears and
          never a jump (ADR 0054): skipping them unasked would end films early.

          An intro skips itself unless the account turned that off (ADR 0083),
          and says so: a jump with a label beside it is not mistaken for a
          broken file, which was 0054's objection, and Watch intro undoes a
          wrong one in a press. After that, or after scrubbing back into an
          intro, the ordinary button is offered instead.

          It sits above the transport rather than among it because it is
          transient: it exists for thirty seconds of a forty-minute episode, and
          a control that comes and goes inside a fixed row makes the row move.

          Outside player__chrome's fade, deliberately. The chrome hides when the
          mouse is still, and a skip that vanished with it would be a button you
          had to wake the interface up to press, during the one stretch nobody
          is touching the mouse.
        */}
        {showUpNext && pb.nextItemID != null ? (
          <UpNextCard
            nextID={pb.nextItemID}
            counting={pb.prefs.autoPlay && pb.playing}
            onTimeout={rollOnUnattended}
            onPlayNow={() => rollOn(true)}
            onCancel={() => setUpNextCancelled(pb.itemID)}
          />
        ) : showIntroSkipped ? (
          <div className="player__skipped" role="status">
            <span>Intro skipped</span>
            <button
              className="player__skip player__skip--inline"
              onClick={() => {
                pb.seekTo(introSkipped.from);
                setIntroSkipped(null);
              }}
            >
              Watch intro
            </button>
          </div>
        ) : skip && (
          <button
            className="player__skip"
            onClick={() => pb.seekTo(skip.atSeconds)}
          >
            {skip.kind === "credits" ? "Skip credits" : "Skip intro"}
          </button>
        )}

        <div className="player__bottom">
          <Scrubber
            current={pb.displayTime}
            duration={pb.totalDuration}
            onSeek={pb.seekTo}
            chapters={chapters}
            previews={previews}
          />
          <div className="player__controls">
            {/* Transport, in the order the hand expects: back through the
                queue, back through the file, play, forward through the file,
                forward through the queue. Skip is inside previous/next because
                it moves within the thing you are watching; the queue buttons
                leave it. */}
            {pb.hasPrev && (
              <button
                className="player__icon"
                onClick={pb.playPrev}
                aria-label="Previous"
                title="Previous"
              >
                <PrevGlyph />
              </button>
            )}
            <button
              className="player__icon"
              onClick={() => pb.seekBy(-10)}
              aria-label="Back 10 seconds"
              title="Back 10 seconds"
            >
              <SkipGlyph dir="back" />
            </button>
            <button
              className="player__icon player__icon--play"
              onClick={pb.togglePlay}
              aria-label={pb.playing ? "Pause" : "Play"}
            >
              {pb.playing ? "❚❚" : "▶"}
            </button>
            <button
              className="player__icon"
              onClick={() => pb.seekBy(10)}
              aria-label="Forward 10 seconds"
              title="Forward 10 seconds"
            >
              <SkipGlyph dir="forward" />
            </button>
            {pb.hasNext && (
              <button
                className="player__icon"
                onClick={pb.playNext}
                aria-label="Next"
                title="Next"
              >
                <NextGlyph />
              </button>
            )}
            {/* Stop ends the session and leaves; close (top left) leaves and
                keeps it playing in the corner. Two different intentions that
                the back arrow alone could not tell apart.

                It sits at the end of the transport group rather than beside
                play, deliberately. Stop tears down a running transcode and
                navigates away — a misfire costs the startup wait again, and
                play is the one control people hit without looking. */}
            <button
              className="player__icon"
              onClick={stopAndLeave}
              aria-label="Stop"
              title="Stop"
            >
              <StopGlyph />
            </button>
            <div className="player__volume">
              <button
                className="player__icon"
                onClick={pb.toggleMute}
                aria-label={pb.muted ? "Unmute" : "Mute"}
              >
                <VolumeGlyph muted={pb.muted || pb.volume === 0} />
              </button>
              <input
                className="player__volume-slider"
                type="range"
                min={0}
                max={1}
                step={0.01}
                value={pb.muted ? 0 : pb.volume}
                onChange={(e) => pb.changeVolume(Number(e.target.value))}
                aria-label="Volume"
                title={`Volume ${Math.round((pb.muted ? 0 : pb.volume) * 100)}%`}
              />
            </div>
            <span className="player__time">
              {clock(pb.displayTime)}{" "}
              <span className="player__time-sep">/</span>{" "}
              {clock(pb.totalDuration)}
            </span>

            <div className="player__spacer" />
            {/* Phones only (Player.css): the way into the options below. */}
            <button
              className={"player__icon player__more" + (moreOpen ? " is-on" : "")}
              onClick={() => setMoreOpen((o) => !o)}
              aria-label="More controls"
              aria-expanded={moreOpen}
              title="More controls"
            >
              ⋯
            </button>
            <div className={"player__options" + (moreOpen ? " is-open" : "")}>
              {/* Shuffle and repeat are queue controls, so they appear wherever
                there is a queue — which in practice is music, but a Play-all
                over a season is the same thing and gets them too. An engaged
                toggle reads through weight and a filled background, never gold:
                gold means focus and nothing else. */}
              {/* Add to playlist. Deliberately outside the queue gate above:
                  a single track put on with no queue behind it is the most
                  ordinary thing to want on a list, and it is the one case that
                  gate excludes. */}
              {item && (
                <button
                  className="player__icon"
                  onClick={() => setAddOpen(true)}
                  aria-label="Add to playlist"
                  title="Add to playlist"
                >
                  +
                </button>
              )}
              {pb.queue.length > 1 && (
                <>
                  <button
                    className={"player__icon" + (pb.shuffle ? " is-on" : "")}
                    onClick={pb.toggleShuffle}
                    aria-label="Shuffle"
                    aria-pressed={pb.shuffle}
                    title={pb.shuffle ? "Shuffle on" : "Shuffle"}
                  >
                    <ShuffleGlyph />
                  </button>
                  <button
                    className={
                      "player__icon" + (pb.repeat !== "off" ? " is-on" : "")
                    }
                    onClick={pb.cycleRepeat}
                    aria-label={`Repeat ${pb.repeat}`}
                    title={
                      pb.repeat === "one"
                        ? "Repeat this one"
                        : pb.repeat === "all"
                          ? "Repeat all"
                          : "Repeat off"
                    }
                  >
                    <RepeatGlyph one={pb.repeat === "one"} />
                  </button>
                  {/* A track's queue is on the page, in Now Playing; the
                      popover is for a Play-all over a season. */}
                  {!pb.isAudio && <div className="player__menu">
                    <button
                      className={"player__icon" + (queueOpen ? " is-on" : "")}
                      onClick={() => setQueueOpen((o) => !o)}
                      aria-label="Queue"
                      aria-expanded={queueOpen}
                      title="Queue"
                    >
                      <QueueGlyph />
                    </button>
                    {queueOpen && (
                      <QueuePanel
                        // The order that will play, not the order it was
                        // gathered in. With shuffle on these differ, and a
                        // panel showing the wrong one is not a cosmetic bug: it
                        // is a list of what is coming next that is wrong about
                        // what is coming next.
                        ids={pb.playOrder}
                        currentID={pb.itemID}
                        // Queued by hand, which is not in playOrder and never
                        // will be — it is a lane beside the queue. Without this
                        // the panel showed the queue a queued track was not
                        // part of, so nothing on screen said it had worked.
                        upNext={pb.upNext}
                        onDrop={pb.removeFromUpNext}
                        onPick={(id, at) => {
                          pb.playFromQueue(id, at);
                          setQueueOpen(false);
                        }}
                      />
                    )}
                  </div>}
                </>
              )}

              {/* Subtitles keep a button of their own, and only they do.
                  Turning subtitles on or off mid-scene is a *transport* action
                  — you missed a line, you want them now — not a settings
                  change, and it is the one thing in the panel frequent enough
                  that two clicks and a scan of a list would be felt. It stays
                  a video affordance: a menu that can only ever say "None" is a
                  control promising something a song cannot give.

                  Which is also why it is gated on there being a track to cycle
                  to. `cycleSub` walks `[null, ...available]`, so with nothing
                  available the click lands and nothing happens — the same empty
                  promise, made by a button instead of a menu. */}
              {/* A track's words are a tab in Now Playing's column, not a
                  button here. */}
              {showsSubtitleButton(pb.isAudio, pb.subtitles) && (
                <div className="player__subs">
                  <button
                    className={"player__icon" + (pb.activeSub ? " is-on" : "")}
                    onClick={() => {
                      pb.cycleSub(1);
                    }}
                    aria-label="Toggle subtitles"
                    aria-pressed={!!pb.activeSub}
                    title={
                      pb.activeSub
                        ? `Subtitles: ${pb.activeSub.label}`
                        : "Subtitles off"
                    }
                  >
                    CC
                  </button>
                </div>
              )}

              {/*
                  Night mode and dialogue boost, one press away. They live in
                  the settings panel too, but you reach for them mid-film, when
                  an explosion has just made you grab the remote, and two
                  clicks into a panel is the wrong distance for that.

                  Offered only where the panel offers them (pb.audioFX: the
                  desktop's own player, from a client that can apply them), and
                  dialogue only off a mono track. Engaged reads through the
                  bar's neutral is-on, never gold (design.md). Dialogue steps
                  Off, Low, High, Off, and carries its level as a letter, since
                  "on" alone cannot tell Low from High.
              */}
              {canNightFilm(pb) && (
                <button
                  className={"player__icon" + (pb.prefs.nightVideo ? " is-on" : "")}
                  onClick={() => pb.setPrefs({ nightVideo: !pb.prefs.nightVideo })}
                  aria-label="Night mode"
                  aria-pressed={pb.prefs.nightVideo}
                  title={pb.prefs.nightVideo ? "Night mode: on" : "Night mode: off"}
                >
                  <NightGlyph />
                </button>
              )}
              {pb.audioFX &&
                !pb.isAudio &&
                canBoostDialogue(pb.audioTracks, pb.audioIndex) && (
                  <button
                    className={
                      "player__icon player__icon--level" +
                      (pb.prefs.dialogueVideo > 0 ? " is-on" : "")
                    }
                    onClick={() =>
                      pb.setPrefs({ dialogueVideo: nextDialogueLevel(pb.prefs.dialogueVideo) })
                    }
                    aria-label={`Dialogue boost: ${DIALOGUE_LEVELS[pb.prefs.dialogueVideo] ?? "Off"}`}
                    aria-pressed={pb.prefs.dialogueVideo > 0}
                    title={`Dialogue boost: ${DIALOGUE_LEVELS[pb.prefs.dialogueVideo] ?? "Off"}`}
                  >
                    <DialogueGlyph />
                    {pb.prefs.dialogueVideo > 0 && (
                      <span className="player__level" aria-hidden="true">
                        {DIALOGUE_LEVELS[pb.prefs.dialogueVideo]?.[0]}
                      </span>
                    )}
                  </button>
                )}

              {/* Music's night mode, on its own preference. Offered where
                  the settings panel offers it (soundControls.ts). */}
              {pb.isAudio && canNightMusic(pb.musicChannels) && (
                <button
                  className={"player__icon" + (pb.prefs.nightMusic ? " is-on" : "")}
                  onClick={() => pb.setPrefs({ nightMusic: !pb.prefs.nightMusic })}
                  aria-label="Night mode"
                  aria-pressed={pb.prefs.nightMusic}
                  title={pb.prefs.nightMusic ? "Night mode: on" : "Night mode: off"}
                >
                  <NightGlyph />
                </button>
              )}

              {/* Everything about *how* this plays, in one place. Engaged when
                  any of it is away from its default, so the strip still says at
                  a glance that something has been changed — which is what the
                  five separate lit-up buttons used to say between them. */}
              <div className="player__menu">
                <button
                  className={
                    "player__icon" +
                    (settingsOpen || settingsChanged ? " is-on" : "")
                  }
                  onClick={() => setSettingsOpen((o) => !o)}
                  aria-label="Playback settings"
                  aria-expanded={settingsOpen}
                  title="Playback settings"
                >
                  <SettingsGlyph />
                </button>
                {settingsOpen && (
                  <PlaybackSettings onClose={() => setSettingsOpen(false)} />
                )}
              </div>
              {/*
                  Pop out. Document picture-in-picture puts *our* player in an
                  always-on-top window (ADR 0029); browser picture-in-picture is
                  the fallback where that API does not exist, and hands the
                  element to the browser along with every control except play
                  and seek. Neither available means no button, which is the rule
                  this bar already follows everywhere else.

                  Audio can pop out too, and only under the document API: video
                  PiP is video-only, while a floating window with cover art,
                  transport and queue is a natural fit for a record.
              */}
              {/*
                  Watch together. Outside the queue-only group it was first put
                  in by mistake — that group is shuffle and repeat, which need
                  something to shuffle, and watching a single film with somebody
                  needs no queue at all. The button was invisible for the most
                  ordinary case there is: one film, two people.

                  Video only. A synchronised record is a real idea and not this
                  one, and offering it on audio would promise something the
                  panel does not do.
              */}
              {!pb.isAudio && (
                <div className="player__menu">
                  <button
                    className={"player__icon" + (togetherOpen ? " is-on" : "")}
                    onClick={() => setTogetherOpen((o) => !o)}
                    aria-label="Watch together"
                    aria-expanded={togetherOpen}
                    title="Watch together"
                  >
                    <TogetherGlyph />
                  </button>
                  {togetherOpen && (
                    <TogetherPanel onClose={() => setTogetherOpen(false)} />
                  )}
                </div>
              )}
              {(pb.popoutAvailable || (!pb.isAudio && pipAvailable)) && (
                <button
                  className={"player__icon" + (pb.popout ? " is-on" : "")}
                  onClick={async () => {
                    if (pb.popoutAvailable) {
                      pb.togglePopout();
                      return;
                    }
                    const v = pb.videoRef.current;
                    if (!v) return;
                    try {
                      if (document.pictureInPictureElement) {
                        await document.exitPictureInPicture();
                      } else {
                        await v.requestPictureInPicture();
                      }
                    } catch {
                      // Refused by the host — a policy decision, not a fault, and
                      // nothing useful to say about it beyond leaving the picture
                      // where it is.
                    }
                  }}
                  aria-label={
                    pb.popoutAvailable ? "Pop out player" : "Picture in picture"
                  }
                  title={
                    pb.popoutAvailable ? "Pop out player" : "Picture in picture"
                  }
                  aria-pressed={pb.popoutAvailable ? pb.popout : undefined}
                >
                  <PipGlyph />
                </button>
              )}
            </div>
            {/* Outside the options group so a phone keeps it in the bar: it is
                the one option somebody reaches for the moment a film starts. */}
            {!pb.isAudio && (
              <button
                className="player__icon"
                onClick={pb.toggleFullscreen}
                aria-label={pb.fullscreen ? "Leave fullscreen" : "Fullscreen"}
                aria-pressed={pb.fullscreen}
                title={pb.fullscreen ? "Leave fullscreen" : "Fullscreen"}
              >
                <FullscreenGlyph />
              </button>
            )}
          </div>
        </div>
      </div>

      {addOpen && item && (
        <AddToPlaylist item={item} onClose={() => setAddOpen(false)} />
      )}
    </div>
  );
}
