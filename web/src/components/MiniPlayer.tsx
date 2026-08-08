import { useNavigate } from "react-router-dom";
import { usePlayback } from "@/playback/PlaybackProvider";
import { useFocusable } from "@/focus/FocusController";
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
        <button
          {...playFocus}
          className="mini__icon"
          onClick={pb.togglePlay}
          aria-label={pb.playing ? "Pause" : "Play"}
        >
          {pb.playing ? "❚❚" : "▶"}
        </button>
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
