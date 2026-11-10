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
 *
 * # What it gained before 1.0
 *
 * A friend's film had only play, the scrubber, volume and fullscreen, and that
 * was reported as missing controls rather than as restraint. Four of the
 * household's are true here too, because they are about this file and this
 * viewer rather than about a queue or a resume: skipping back and forward,
 * subtitles, another audio track, and skipping the intro and the credits. The
 * tracks and markers come from the friend's server (`audio`, `markers` on
 * their item answer); a server too old to send them simply offers none.
 *
 * In a room the skips and the intro/credits buttons follow play and the
 * scrubber (the host drives); subtitles and the audio track stay the viewer's
 * own, as volume does.
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
  onSkip,
  subtitles = [],
  subtitle = null,
  onSubtitle,
  audio = [],
  audioTrack = null,
  onAudio,
  skip = null,
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
  /** Move by this many seconds: -10 back, 10 forward. */
  onSkip?: (by: number) => void;
  /** The subtitle tracks to choose from, and the chosen one's key or null. */
  subtitles?: { key: string; label: string }[];
  subtitle?: string | null;
  onSubtitle?: (key: string | null) => void;
  /** The audio tracks, by absolute stream index, and the chosen one. */
  audio?: { index: number; label: string }[];
  audioTrack?: number | null;
  onAudio?: (index: number) => void;
  /** "Skip intro" or "Skip credits" while inside one, with where it goes. */
  skip?: { label: string; onSkip: () => void } | null;
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

      {onSkip && (
        <button
          type="button"
          className="peer-controls__button peer-controls__skip"
          onClick={() => onSkip(-10)}
          disabled={!seekable}
          aria-label="Back 10 seconds"
          title="Back 10 seconds (←)"
        >
          −10
        </button>
      )}
      {onSkip && (
        <button
          type="button"
          className="peer-controls__button peer-controls__skip"
          onClick={() => onSkip(10)}
          disabled={!seekable}
          aria-label="Forward 10 seconds"
          title="Forward 10 seconds (→)"
        >
          +10
        </button>
      )}

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

      {/* Over the scrubber's end rather than over the picture: this screen has
          no overlay chrome to put it in, and here it is where the eye already
          is when the intro starts. */}
      {skip && !following && (
        <button type="button" className="peer-controls__skip-marker" onClick={skip.onSkip}>
          {skip.label}
        </button>
      )}

      {/*
        Native selects: keyboard- and remote-operable as they are, and a screen
        reader knows what each one is. Only drawn when there is a choice.
      */}
      {subtitles.length > 0 && onSubtitle && (
        <select
          className="peer-controls__select"
          value={subtitle ?? ""}
          onChange={(e) => onSubtitle(e.currentTarget.value === "" ? null : e.currentTarget.value)}
          aria-label="Subtitles"
          title="Subtitles"
        >
          <option value="">Subtitles off</option>
          {subtitles.map((s) => (
            <option key={s.key} value={s.key}>
              {s.label}
            </option>
          ))}
        </select>
      )}
      {audio.length > 1 && onAudio && (
        <select
          className="peer-controls__select"
          value={audioTrack ?? audio[0].index}
          onChange={(e) => onAudio(Number(e.currentTarget.value))}
          aria-label="Audio"
          title="Audio"
        >
          {audio.map((a) => (
            <option key={a.index} value={a.index}>
              {a.label}
            </option>
          ))}
        </select>
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
