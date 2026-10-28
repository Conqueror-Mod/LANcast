import { useRef, useState } from "react";
import { chapterAt, type ChapterMark } from "@/lib/chapters";
import { frameAt, type PreviewSheets } from "@/lib/previews";
import "./Scrubber.css";

interface Props {
  current: number;
  duration: number;
  onSeek: (seconds: number) => void;
  /** The file's chapters, drawn as ticks and named under the pointer. */
  chapters?: ChapterMark[];
  /**
   * The file's preview sheets (ADR 0105) and where sheet n is, shown as the
   * frame under the pointer. Absent until the background pass has made them.
   */
  previews?: { sheets: PreviewSheets; url: (sheet: number) => string } | null;
}

// A hairline that thickens on hover, filled gold. Seeking commits on release
// rather than during the drag, so scrubbing across a transcode does not restart
// ffmpeg on every pixel of movement.
//
// Chapter ticks are a neutral white, never gold: gold says where you are, and a
// row of gold marks would say it in twenty places at once.
export function Scrubber({ current, duration, onSeek, chapters = [], previews }: Props) {
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
  // The frame under the pointer, while there is a pointer and sheets to
  // take it from. Half size on a phone, where the bar is the screen's width.
  const frame =
    previews && duration > 0 && pointerRatio !== null
      ? frameAt(previews.sheets, pointerRatio * duration, duration)
      : null;
  const scale =
    previews && typeof window !== "undefined" && window.innerWidth < 600
      ? Math.min(1, 160 / previews.sheets.width)
      : 1;
  const frameW = previews ? Math.round(previews.sheets.width * scale) : 0;
  const frameH = previews ? Math.round(previews.sheets.height * scale) : 0;

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
      {frame && previews && pointerRatio !== null && (
        // Clamped like the label, by half the frame, so neither end hangs off
        // the screen. A window onto a sheet already loaded: moving the
        // pointer moves the window, it does not fetch an image per pixel.
        <div
          className="scrubber__preview"
          data-testid="scrub-preview"
          aria-hidden="true"
          style={{
            left: `clamp(${frameW / 2}px, ${pointerRatio * 100}%, calc(100% - ${frameW / 2}px))`,
            width: frameW,
            height: frameH,
            backgroundImage: `url("${previews.url(frame.sheet)}")`,
            backgroundPosition: `-${frame.x * scale}px -${frame.y * scale}px`,
            backgroundSize: `${previews.sheets.columns * frameW}px ${previews.sheets.rows * frameH}px`,
          }}
        />
      )}
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
