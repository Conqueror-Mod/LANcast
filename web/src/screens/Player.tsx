import { useCallback, useEffect, useRef, useState } from "react";
import {
  useLocation,
  useNavigate,
  useParams,
  useSearchParams,
} from "react-router-dom";
import { useBackHandler, useSuspendFocus } from "@/focus/FocusController";
import { clock } from "@/lib/format";
import { showsSubtitleButton } from "@/lib/subtitleButton";
import { matchesBinding, bindingKeys } from "@/lib/keys";
import { Scrubber } from "@/components/Scrubber";
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
  PipGlyph,
  FullscreenGlyph,
  PrevGlyph,
  NextGlyph,
  StopGlyph,
} from "@/components/PlayerGlyphs";
import { usePlayback, useFullSurface } from "@/playback/PlaybackProvider";
import { DEFAULTS } from "@/playback/prefs";
import "./Player.css";

// The player screen is chrome. The media element and everything that drives it
// live in PlaybackProvider, above the router, so that leaving this screen docks
// the player instead of stopping it (docs/music-client-plan.md).
//
// What remains here is what belongs to *being on the player screen*: the
// transport, the idle auto-hide, the subtitle menu, and the keyboard surface.
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
  } | null;
  const stateQueue = handoff?.queue;
  const stateShuffle = handoff?.shuffle;

  const queueParam = searchParams.get("queue");
  const { play, setShuffle } = pb;
  useEffect(() => {
    if (!itemID) return;
    const queue =
      stateQueue && stateQueue.length > 0
        ? stateQueue
        : queueParam
          ? queueParam.split(",").map(Number)
          : [itemID];
    play(itemID, queue);
    // Only when the caller said so. Shuffle otherwise belongs to the session —
    // starting an album should not silently clear a shuffle you had turned on.
    if (stateShuffle !== undefined) setShuffle(stateShuffle);
    // stateQueue is stable for a history entry; its length identifies it well
    // enough to avoid re-running on an unrelated render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [itemID, queueParam, play, setShuffle, stateQueue?.length, stateShuffle]);

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
  const back = useCallback(() => {
    if (pb.fullscreen) {
      pb.exitFullscreen();
      return;
    }
    close();
  }, [pb, close]);
  useBackHandler(back);

  // ---- auto-hide chrome -----------------------------------------------------
  const idleTimer = useRef<number>();
  // Set by a click on the picture, cancelled by a second one. See the handlers.
  const clickTimer = useRef<number>();
  const wakeChrome = useCallback(() => {
    setChromeVisible(true);
    window.clearTimeout(idleTimer.current);
    // Audio keeps its controls. Chrome hides to get out of the way of the
    // picture; over a still cover there is nothing to get out of the way of,
    // and hiding the transport would leave a motionless screen that looks
    // frozen rather than playing.
    if (pb.isAudio) return;
    idleTimer.current = window.setTimeout(() => {
      if (pb.playing) setChromeVisible(false);
    }, 2500);
  }, [pb.isAudio, pb.playing]);

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
  } = pb;
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // While typing in the subtitle search box, keys are text — not transport.
      const ae = document.activeElement;
      if (
        ae instanceof HTMLElement &&
        (ae.tagName === "INPUT" ||
          ae.tagName === "TEXTAREA" ||
          ae.isContentEditable)
      ) {
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
        if (e.key === subs[0]) cycleSub(-1);
        else if (e.key === subs[1]) cycleSub(1);
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
    pb.muted,
    pb.volume,
    wakeChrome,
  ]);

  const item = pb.item;

  return (
    <div
      className={
        "player" +
        (chromeVisible ? "" : " player--idle") +
        (pb.isAudio ? " player--audio" : "")
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

      <div className="player__chrome">
        <div className="player__top">
          <button className="player__icon" onClick={close} aria-label="Close">
            ←
          </button>
          {/* A song is identified by three things, not one. `series` is the
              album on a track (ADR 0024), and `artist` the performer — which on
              a compilation is not the album artist, so it is worth showing. */}
          <span className="player__title">
            {item?.title}
            {pb.isAudio && (item?.artist || item?.series) && (
              <span className="player__subtitle">
                {[item?.artist, item?.series].filter(Boolean).join(" — ")}
              </span>
            )}
          </span>
          {pb.note && <span className="player__note">{pb.note}</span>}
        </div>

        <div className="player__bottom">
          <Scrubber
            current={pb.displayTime}
            duration={pb.totalDuration}
            onSeek={pb.seekTo}
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
            <div className="player__options">
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
                  <div className="player__menu">
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
                        onPick={(id, at) => {
                          pb.playFromQueue(id, at);
                          setQueueOpen(false);
                        }}
                      />
                    )}
                  </div>
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
                  aria-label={pb.popoutAvailable ? "Pop out player" : "Picture in picture"}
                  title={pb.popoutAvailable ? "Pop out player" : "Picture in picture"}
                  aria-pressed={pb.popoutAvailable ? pb.popout : undefined}
                >
                  <PipGlyph />
                </button>
              )}
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
      </div>

      {addOpen && item && (
        <AddToPlaylist item={item} onClose={() => setAddOpen(false)} />
      )}
    </div>
  );
}
