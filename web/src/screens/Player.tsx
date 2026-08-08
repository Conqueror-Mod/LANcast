import { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { useBackHandler, useSuspendFocus } from "@/focus/FocusController";
import { clock } from "@/lib/format";
import { Scrubber } from "@/components/Scrubber";
import { SubtitleMenu } from "@/components/SubtitleMenu";
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

  // Leaving the player leaves it playing. That is the point of the change, and
  // it is why Back no longer stops anything.
  const close = useCallback(() => navigate(-1), [navigate]);
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
  const { togglePlay, toggleFullscreen, toggleMute, seekBy, cycleSub, changeVolume } = pb;
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // While typing in the subtitle search box, keys are text — not transport.
      const ae = document.activeElement;
      if (
        ae instanceof HTMLElement &&
        (ae.tagName === "INPUT" || ae.tagName === "TEXTAREA" || ae.isContentEditable)
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
            <button
              className="player__icon player__icon--play"
              onClick={pb.togglePlay}
              aria-label={pb.playing ? "Pause" : "Play"}
            >
              {pb.playing ? "❚❚" : "▶"}
            </button>
            <div className="player__volume">
              <button
                className="player__icon"
                onClick={pb.toggleMute}
                aria-label={pb.muted ? "Unmute" : "Mute"}
              >
                {pb.muted || pb.volume === 0 ? "🔇" : "🔊"}
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
              {clock(pb.displayTime)} <span className="player__time-sep">/</span>{" "}
              {clock(pb.totalDuration)}
            </span>

            {/* Subtitles and fullscreen are video affordances. A subtitle menu
                that can only ever say "none", and a fullscreen button for a
                still image, are both controls that promise something the
                content cannot give. */}
            {!pb.isAudio && (
              <div className="player__subs player__icon--right">
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
            {!pb.isAudio && (
              <button
                className="player__icon"
                onClick={pb.toggleFullscreen}
                aria-label="Fullscreen"
              >
                ⛶
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
