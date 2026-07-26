import { useRef, useState } from "react";
import "./Scrubber.css";

interface Props {
  current: number;
  duration: number;
  onSeek: (seconds: number) => void;
}

// A hairline that thickens on hover, filled gold. Seeking commits on release
// rather than during the drag, so scrubbing across a transcode does not restart
// ffmpeg on every pixel of movement.
export function Scrubber({ current, duration, onSeek }: Props) {
  const trackRef = useRef<HTMLDivElement>(null);
  const [dragging, setDragging] = useState(false);
  const [preview, setPreview] = useState(0);

  const ratio = duration > 0 ? (dragging ? preview : current / duration) : 0;
  const pct = Math.max(0, Math.min(1, ratio)) * 100;

  const ratioAt = (clientX: number) => {
    const el = trackRef.current;
    if (!el) return 0;
    const r = el.getBoundingClientRect();
    return Math.max(0, Math.min(1, (clientX - r.left) / r.width));
  };

  return (
    <div
      ref={trackRef}
      className={"scrubber" + (dragging ? " is-dragging" : "")}
      role="slider"
      aria-label="Seek"
      aria-valuemin={0}
      aria-valuemax={Math.round(duration)}
      aria-valuenow={Math.round(current)}
      onPointerDown={(e) => {
        e.currentTarget.setPointerCapture(e.pointerId);
        setDragging(true);
        setPreview(ratioAt(e.clientX));
      }}
      onPointerMove={(e) => {
        if (dragging) setPreview(ratioAt(e.clientX));
      }}
      onPointerUp={(e) => {
        if (!dragging) return;
        const r = ratioAt(e.clientX);
        setDragging(false);
        if (duration > 0) onSeek(r * duration);
      }}
    >
      <div className="scrubber__track">
        <div className="scrubber__fill" style={{ width: `${pct}%` }} />
        <div className="scrubber__knob" style={{ left: `${pct}%` }} />
      </div>
    </div>
  );
}
