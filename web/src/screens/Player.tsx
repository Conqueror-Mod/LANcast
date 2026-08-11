import { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { useBackHandler, useSuspendFocus } from "@/focus/FocusController";
import { clock } from "@/lib/format";
import { Scrubber } from "@/components/Scrubber";
import { SubtitleMenu } from "@/components/SubtitleMenu";
import { QueuePanel, audioLabel } from "@/components/QueuePanel";
import { SkipGlyph } from "@/components/SkipGlyph";
import {
  ShuffleGlyph,
  RepeatGlyph,
  VolumeGlyph,
  AudioTrackGlyph,
  QueueGlyph,
  PipGlyph,
  FullscreenGlyph,
  PrevGlyph,
  NextGlyph,
  StopGlyph,
} from "@/components/PlayerGlyphs";
import { usePlayback, useFullSurface } from "@/playback/PlaybackProvider";
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
  const queueParam = searchParams.get("queue");
  const { play } = pb;
  useEffect(() => {
    if (!itemID) return;
    const queue = queueParam ? queueParam.split(",").map(Number) : [itemID];
    play(itemID, queue);
  }, [itemID, queueParam, play]);

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
  const [subMenuOpen, setSubMenuOpen] = useState(false);
  const [speedOpen, setSpeedOpen] = useState(false);
  const [audioOpen, setAudioOpen] = useState(false);
  const [queueOpen, setQueueOpen] = useState(false);
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

  /*
   * Which audio track is actually playing.
   *
   * `audioIndex` is null until you choose one, meaning "whatever the file leads
   * with" — but that is still a real track, and something is coming out of the
   * speakers. Marking the current row from `audioIndex` alone left *nothing*
   * ticked the first time the picker was opened, which is every time on a file
   * you have not already fiddled with: two tracks offered and no indication of
   * which one you are listening to. The point of the picker is telling them
   * apart.
   *
   * The file's own default is the stream flagged `default`, not simply the
   * first one — a release with a commentary track first and the feature audio
   * second is unusual but entirely legal. First is the fallback when nothing
   * carries the flag.
   */
  const defaultAudio =
    pb.audioTracks.find((t) => t.default) ?? pb.audioTracks[0];
  const currentAudio = pb.audioIndex ?? defaultAudio?.index;

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
  useBackHandler(close);

  // ---- auto-hide chrome -----------------------------------------------------
  const idleTimer = useRef<number>();
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

  useEffect(() => () => window.clearTimeout(idleTimer.current), []);

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
      switch (e.key) {
        case " ":
        case "k":
          e.preventDefault();
          togglePlay();
          break;
        case "f":
          toggleFullscreen();
          break;
        case "m":
          toggleMute();
          break;
        case "ArrowLeft":
          e.preventDefault();
          seekBy(-10);
          break;
        case "ArrowRight":
          e.preventDefault();
          seekBy(10);
          break;
        case "ArrowUp":
          e.preventDefault();
          changeVolume((pb.muted ? 0 : pb.volume) + 0.05);
          break;
        case "ArrowDown":
          e.preventDefault();
          changeVolume((pb.muted ? 0 : pb.volume) - 0.05);
          break;
        case "[":
          cycleSub(-1);
          break;
        case "]":
          cycleSub(1);
          break;
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
        if (e.target === e.currentTarget) pb.togglePlay();
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
                        ids={pb.queue}
                        currentID={pb.itemID}
                        onPick={(id) => {
                          pb.playFromQueue(id);
                          setQueueOpen(false);
                        }}
                      />
                    )}
                  </div>
                </>
              )}

              {/* Speed is offered everywhere: a slow talker is as common in a
                podcast-length album track as in a documentary. */}
              <div className="player__menu">
                <button
                  className={"player__icon" + (pb.speed !== 1 ? " is-on" : "")}
                  onClick={() => setSpeedOpen((o) => !o)}
                  aria-label="Playback speed"
                  aria-expanded={speedOpen}
                  title={`Speed ${pb.speed}×`}
                >
                  {pb.speed}×
                </button>
                {speedOpen && (
                  <div className="player__pop" role="menu">
                    {[0.5, 0.75, 1, 1.25, 1.5, 1.75, 2].map((r) => (
                      <button
                        key={r}
                        role="menuitemradio"
                        aria-checked={pb.speed === r}
                        className={
                          "player__pop-item" + (pb.speed === r ? " is-on" : "")
                        }
                        onClick={() => {
                          pb.setSpeed(r);
                          setSpeedOpen(false);
                        }}
                      >
                        {r === 1 ? "Normal" : `${r}×`}
                      </button>
                    ))}
                  </div>
                )}
              </div>

              {/* Only when the file actually carries a choice. A picker listing
                one track is a control that cannot do anything. */}
              {pb.audioTracks.length > 1 && (
                <div className="player__menu">
                  <button
                    /* Engaged means "not the track this file leads with", so
                       explicitly choosing the default does not light it. */
                    className={
                      "player__icon" +
                      (currentAudio !== defaultAudio?.index ? " is-on" : "")
                    }
                    onClick={() => setAudioOpen((o) => !o)}
                    aria-label="Audio track"
                    aria-expanded={audioOpen}
                    title="Audio track"
                  >
                    <AudioTrackGlyph />
                  </button>
                  {audioOpen && (
                    <div className="player__pop" role="menu">
                      {pb.audioTracks.map((t) => (
                        <button
                          key={t.index}
                          role="menuitemradio"
                          aria-checked={currentAudio === t.index}
                          className={
                            "player__pop-item" +
                            (currentAudio === t.index ? " is-on" : "")
                          }
                          onClick={() => {
                            pb.selectAudio(t.index);
                            setAudioOpen(false);
                          }}
                        >
                          {audioLabel(t)}
                        </button>
                      ))}
                    </div>
                  )}
                </div>
              )}

              {/* Subtitles and fullscreen are video affordances. A subtitle menu
                that can only ever say "none", and a fullscreen button for a
                still image, are both controls that promise something the
                content cannot give. */}
              {!pb.isAudio && (
                <div className="player__subs">
                  <button
                    className={"player__icon" + (pb.activeSub ? " is-on" : "")}
                    onClick={() => setSubMenuOpen((o) => !o)}
                    aria-label="Subtitles"
                    aria-expanded={subMenuOpen}
                  >
                    CC
                  </button>
                  {subMenuOpen && (
                    <SubtitleMenu
                      itemID={pb.itemID}
                      itemTitle={item?.title ?? ""}
                      language="en"
                      tracks={pb.subtitles}
                      activeKey={pb.subKey}
                      onSelect={(key) => {
                        pb.selectSub(key);
                        setSubMenuOpen(false);
                      }}
                    />
                  )}
                </div>
              )}
              {!pb.isAudio && pipAvailable && (
                <button
                  className="player__icon"
                  onClick={async () => {
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
                  aria-label="Picture in picture"
                  title="Picture in picture"
                >
                  <PipGlyph />
                </button>
              )}
              {!pb.isAudio && (
                <button
                  className="player__icon"
                  onClick={pb.toggleFullscreen}
                  aria-label="Fullscreen"
                >
                  <FullscreenGlyph />
                </button>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
