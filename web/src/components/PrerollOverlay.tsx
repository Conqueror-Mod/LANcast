import { useEffect, useRef } from "react";
import "./PrerollOverlay.css";

/*
 * The pre-roll clip, over the player, before the film starts (ADR 0099).
 *
 * A plain video element: the clip plays as a file, nothing about it is
 * recorded, and the film's own player has not been handed anything yet. It
 * ends in one of three ways and all three start the film — the clip finished,
 * the person skipped it, or it could not play. A clip that cannot play is not
 * an error worth showing: it stands between somebody and their film.
 *
 * Escape and the Skip button skip. Keys are taken in the capture phase so the
 * player's own keys (space, the arrows) do not act on a film not yet started.
 */
export function PrerollOverlay({ src, onDone }: { src: string; onDone: () => void }) {
  const done = useRef(false);
  const finish = () => {
    if (done.current) return;
    done.current = true;
    onDone();
  };

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // Immediate: the player listens on window too, and stopPropagation alone
      // does not stop other listeners on the same target.
      e.stopImmediatePropagation();
      if (e.key === "Escape" || e.key === "Enter") {
        e.preventDefault();
        finish();
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div className="preroll" role="dialog" aria-label="Pre-roll">
      <video
        className="preroll__video"
        src={src}
        autoPlay
        playsInline
        onEnded={finish}
        onError={finish}
      />
      <button className="preroll__skip" onClick={finish} autoFocus>
        Skip
      </button>
    </div>
  );
}
