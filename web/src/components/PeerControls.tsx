import { VolumeGlyph, FullscreenGlyph } from "./PlayerGlyphs";
import "./PeerControls.css";

/*
 * Controls for a film on somebody else's server (ADR 0071 §5).
 *
 * # Why this exists rather than the element's own
 *
 * Native controls show the **element's** timeline. On a converted stream that
 * is not the film's: each session starts at zero and reports only what has been
 * produced so far, so after seeking to forty minutes the scrubber reads nought
 * against a duration of a few seconds. It looked like seeking had broken the
 * film, which is how it was reported.
 *
 * # Why not the household player's chrome
 *
 * That is built around the playback provider — a queue, a resume position,
 * progress writes, an up-next lane — and a peer item may touch none of it
 * (ADR 0071 §5, §4). Borrowing it would mean drawing furniture over state
 * nothing is keeping, which is the merging §5 forbids done with CSS.
 *
 * So: the things that are true here and nothing else. Volume and fullscreen
 * belong on that list because they are about the person watching, not about
 * the film — they are true on any film, in any room, whoever drives.
 *
 * # In a room
 *
 * The host drives (ADR 0046 §9), so play and the scrubber are shown disabled
 * rather than live-but-ignored: a button that looks pressable and does nothing
 * reads as broken. Volume, mute and fullscreen stay live — nobody else's
 * evening is affected by how loud yours is.
 */
export function PeerControls({
  paused,
  at,
  total,
  converting,
  onPlayPause,
  onSeek,
  following = false,
  volume,
  muted,
  onVolume,
  onToggleMute,
  fullscreen,
  onToggleFullscreen,
}: {
  paused: boolean;
  at: number;
  total: number;
  converting: boolean;
  onPlayPause: () => void;
  onSeek: (to: number) => void;
  /** In a room as a guest: the host drives play and position. */
  following?: boolean;
  volume: number;
  muted: boolean;
  onVolume: (v: number) => void;
  onToggleMute: () => void;
  fullscreen: boolean;
  onToggleFullscreen: () => void;
}) {
  const seekable = total > 0 && !following;
  const silent = muted || volume === 0;

  return (
    <div className="peer-controls">
      <button
        type="button"
        className="peer-controls__play"
        onClick={onPlayPause}
        disabled={following}
        title={following ? "The host controls playback" : undefined}
        aria-label={paused ? "Play" : "Pause"}
      >
        {paused ? "▶" : "❚❚"}
      </button>

      <span className="peer-controls__time">{clock(at)}</span>

      {/*
        A range input rather than a drawn bar: it is keyboard-operable, it is
        draggable, and a screen reader knows what it is — none of which a div
        with a background gradient gets for free.

        Committed on change rather than on input, because a converted seek asks
        the far server to start encoding somewhere else. Firing that for every
        pixel of a drag would start a session per pixel.
      */}
      <input
        className="peer-controls__bar"
        type="range"
        min={0}
        max={total > 0 ? Math.floor(total) : 0}
        value={Math.floor(Math.min(at, total > 0 ? total : at))}
        disabled={!seekable}
        onChange={(e) => onSeek(Number(e.currentTarget.value))}
        aria-label="Position"
      />

      <span className="peer-controls__time">
        {total > 0 ? clock(total) : "--:--"}
      </span>

      {/*
        Said once, quietly, and only where it is true.

        A converted seek is a request to another household's machine to start
        again somewhere else, so it takes a moment in a way a normal scrubber
        does not. Somebody who knows that reads a pause as the thing working.
      */}
      {converting && (
        <span className="peer-controls__note" title="Converted on their machine">
          converting
        </span>
      )}

      <button
        type="button"
        className="peer-controls__button peer-controls__mute"
        onClick={onToggleMute}
        aria-label={silent ? "Unmute" : "Mute"}
        aria-pressed={silent}
      >
        <VolumeGlyph muted={silent} />
      </button>
      <input
        className="peer-controls__volume"
        type="range"
        min={0}
        max={100}
        // Shows zero while muted, so the slider and the button never disagree
        // about whether anything is coming out.
        value={silent ? 0 : Math.round(volume * 100)}
        onChange={(e) => onVolume(Number(e.currentTarget.value) / 100)}
        aria-label="Volume"
      />
      <button
        type="button"
        className="peer-controls__button"
        onClick={onToggleFullscreen}
        aria-label={fullscreen ? "Exit fullscreen" : "Fullscreen"}
        aria-pressed={fullscreen}
      >
        <FullscreenGlyph />
      </button>
    </div>
  );
}

/** h:mm:ss, dropping the hours on anything under one. */
function clock(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return "0:00";
  const s = Math.floor(seconds % 60);
  const m = Math.floor((seconds / 60) % 60);
  const h = Math.floor(seconds / 3600);
  const mm = h > 0 ? String(m).padStart(2, "0") : String(m);
  return `${h > 0 ? `${h}:` : ""}${mm}:${String(s).padStart(2, "0")}`;
}
