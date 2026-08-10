import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { usePlayback } from "@/playback/PlaybackProvider";
import { useFocusable } from "@/focus/FocusController";
import { PrevGlyph, NextGlyph, VolumeGlyph } from "@/components/PlayerGlyphs";
import "./MiniPlayer.css";

// The docked player, bottom-right, when something is playing and you are not on
// the player screen.
//
// Why it exists: leaving the player used to stop the sound. Right for a film,
// which you watch and finish; wrong for a record, which you put on and then go
// and browse — the ordinary case for music, not an advanced one.
//
// It is controls only. The picture (or the cover) is the provider's surface,
// which docks itself into the same corner — one media element, moved with CSS,
// because re-parenting it in React would unmount it and unmounting stops the
// sound.
export function MiniPlayer() {
  const pb = usePlayback();
  const navigate = useNavigate();

  const expand = () => navigate(`/watch/${pb.itemID}`);
  const expandFocus = useFocusable(expand);
  const playFocus = useFocusable(pb.togglePlay);
  const stopFocus = useFocusable(pb.stop);
  const prevFocus = useFocusable(pb.playPrev);
  const nextFocus = useFocusable(pb.playNext);
  const [volOpen, setVolOpen] = useState(false);
  const volFocus = useFocusable(() => setVolOpen((o) => !o));

  if (pb.surface !== "mini") return null;

  const item = pb.item;
  // On a track the album artist is worth more than the album title in a strip
  // this small: it says whose record is on.
  const secondary = pb.isAudio
    ? [item?.artist, item?.series].filter(Boolean).join(" — ")
    : "";

  return (
    <div className="mini" role="region" aria-label="Now playing">
      <button
        {...expandFocus}
        className="mini__open"
        onClick={expand}
        title="Back to the player"
      >
        <span className="mini__title">{item?.title ?? "Playing"}</span>
        {secondary && <span className="mini__sub">{secondary}</span>}
      </button>

      <div className="mini__controls">
        {/* Track navigation is the point of a mini-player: moving through a
            record without leaving the page you are on. Hidden rather than
            disabled when there is no queue — a permanently dead button in a
            180px strip is spent space. */}
        {pb.hasPrev && (
          <button
            {...prevFocus}
            className="mini__icon"
            onClick={pb.playPrev}
            aria-label="Previous"
            title="Previous"
          >
            <PrevGlyph size={16} />
          </button>
        )}
        <button
          {...playFocus}
          className="mini__icon"
          onClick={pb.togglePlay}
          aria-label={pb.playing ? "Pause" : "Play"}
        >
          {pb.playing ? "❚❚" : "▶"}
        </button>
        {pb.hasNext && (
          <button
            {...nextFocus}
            className="mini__icon"
            onClick={pb.playNext}
            aria-label="Next"
            title="Next"
          >
            <NextGlyph size={16} />
          </button>
        )}
        {/* Volume as a button that reveals a slider, not a slider always shown.
            A permanent slider is the single widest thing that could go in this
            strip, and the strip sits over content. */}
        <div className="mini__volume">
          <button
            {...volFocus}
            className="mini__icon"
            onClick={() => setVolOpen((o) => !o)}
            aria-label="Volume"
            aria-expanded={volOpen}
            title={`Volume ${Math.round((pb.muted ? 0 : pb.volume) * 100)}%`}
          >
            <VolumeGlyph size={16} muted={pb.muted || pb.volume === 0} />
          </button>
          {volOpen && (
            <div className="mini__volume-pop">
              <button
                className="mini__icon"
                onClick={pb.toggleMute}
                aria-label={pb.muted ? "Unmute" : "Mute"}
              >
                <VolumeGlyph size={16} muted={pb.muted || pb.volume === 0} />
              </button>
              <input
                className="mini__volume-slider"
                type="range"
                min={0}
                max={1}
                step={0.01}
                value={pb.muted ? 0 : pb.volume}
                onChange={(e) => pb.changeVolume(Number(e.target.value))}
                aria-label="Volume"
              />
            </div>
          )}
        </div>
        <button
          {...stopFocus}
          className="mini__icon"
          onClick={pb.stop}
          aria-label="Stop"
          title="Stop"
        >
          ✕
        </button>
      </div>

      {/* A progress hairline rather than a scrubber. The strip is too small to
          seek in accurately, and a mis-seek in a corner widget is worse than no
          seek at all — the full player is one click away. */}
      <div
        className="mini__progress"
        style={{
          width: pb.totalDuration
            ? `${Math.min(100, (pb.displayTime / pb.totalDuration) * 100)}%`
            : "0%",
        }}
      />
    </div>
  );
}
