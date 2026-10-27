import { useRef, useState } from "react";
import { chapterAt, type ChapterMark } from "@/lib/chapters";
import "./Scrubber.css";

interface Props {
  current: number;
  duration: number;
  onSeek: (seconds: number) => void;
  /** The file's chapters, drawn as ticks and named under the pointer. */
  chapters?: ChapterMark[];
}

// A hairline that thickens on hover, filled gold. Seeking commits on release
// rather than during the drag, so scrubbing across a transcode does not restart
// ffmpeg on every pixel of movement.
//
// Chapter ticks are a neutral white, never gold: gold says where you are, and a
// row of gold marks would say it in twenty places at once.
export function Scrubber({ current, duration, onSeek, chapters = [] }: Props) {
  const trackRef = useRef<HTMLDivElement>(null);
  const [dragging, setDragging] = useState(false);
  const [preview, setPreview] = useState(0);
  // Where the pointer is while hovering, as a ratio; null when it is not over.
  const [hover, setHover] = useState<number | null>(null);

  const ratio = duration > 0 ? (dragging ? preview : current / duration) : 0;
  const pct = Math.max(0, Math.min(1, ratio)) * 100;

  const ratioAt = (clientX: number) => {
    const el = trackRef.current;
    if (!el) return 0;
    const r = el.getBoundingClientRect();
    return r.width > 0 ? Math.max(0, Math.min(1, (clientX - r.left) / r.width)) : 0;
  };

  // The chapter named under the pointer: where a drag is, else where it hovers.
  const pointerRatio = dragging ? preview : hover;
  const named =
    duration > 0 && pointerRatio !== null && chapters.length > 0
      ? chapterAt(chapters, pointerRatio * duration)
      : undefined;
  const playing = duration > 0 ? chapterAt(chapters, current) : undefined;

  return (
    <div
      ref={trackRef}
      className={"scrubber" + (dragging ? " is-dragging" : "")}
      role="slider"
      aria-label="Seek"
      aria-valuemin={0}
      aria-valuemax={Math.round(duration)}
      aria-valuenow={Math.round(current)}
      aria-valuetext={playing ? `${Math.round(current)} seconds, ${playing.title}` : undefined}
      onPointerDown={(e) => {
        e.currentTarget.setPointerCapture(e.pointerId);
        setDragging(true);
        setPreview(ratioAt(e.clientX));
      }}
      onPointerMove={(e) => {
        if (dragging) setPreview(ratioAt(e.clientX));
        else setHover(ratioAt(e.clientX));
      }}
      onPointerLeave={() => setHover(null)}
      onPointerUp={(e) => {
        if (!dragging) return;
        const r = ratioAt(e.clientX);
        setDragging(false);
        if (duration > 0) onSeek(r * duration);
      }}
    >
      <div className="scrubber__track">
        <div className="scrubber__fill" style={{ width: `${pct}%` }} />
        {duration > 0 &&
          chapters
            .filter((c) => c.start_ms > 0 && c.start_ms / 1000 < duration)
            .map((c) => (
              <div
                key={c.start_ms}
                className="scrubber__mark"
                data-testid="chapter-mark"
                style={{ left: `${(c.start_ms / 1000 / duration) * 100}%` }}
              />
            ))}
        <div className="scrubber__knob" style={{ left: `${pct}%` }} />
      </div>
      {named && pointerRatio !== null && (
        // Clamped so a label near either end does not hang off the screen.
        <div
          className="scrubber__label"
          style={{ left: `clamp(70px, ${pointerRatio * 100}%, calc(100% - 70px))` }}
        >
          {named.title}
        </div>
      )}
    </div>
  );
}
