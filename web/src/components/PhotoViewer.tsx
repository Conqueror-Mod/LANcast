import { useCallback, useEffect, useRef, useState } from "react";
import { useBackHandler } from "@/focus/FocusController";
import type { Item } from "@/api/types";
import "./PhotoViewer.css";

const SLIDESHOW_MS = 5000;
const MAX_ZOOM = 6;

// Full-screen picture viewer (ADR 0028).
//
// It owns the keyboard while it is open, which is a deliberate exception to the
// spatial focus controller rather than an oversight: arrows here mean "previous
// and next picture", not "move focus left and right", and there is nothing else
// on screen to move focus to. Escape returns focus to whatever opened it, so the
// page underneath is never left with focus on the body — the one thing ADR 0004
// says must not happen.
export function PhotoViewer({
  photos,
  startAt,
  onClose,
}: {
  photos: Item[];
  startAt: number;
  onClose: () => void;
}) {
  const [index, setIndex] = useState(startAt);
  const [zoom, setZoom] = useState(1);
  const [pan, setPan] = useState({ x: 0, y: 0 });
  const [playing, setPlaying] = useState(false);
  const drag = useRef<{ x: number; y: number; px: number; py: number } | null>(
    null,
  );
  const closeRef = useRef<HTMLButtonElement | null>(null);

  useBackHandler(onClose);

  const item = photos[index];

  const reset = useCallback(() => {
    setZoom(1);
    setPan({ x: 0, y: 0 });
  }, []);

  const go = useCallback(
    (delta: number) => {
      if (photos.length === 0) return;
      setIndex((i) => (i + delta + photos.length) % photos.length);
      // Zoom belongs to the picture being looked at, not to the viewer. Keeping
      // it across a move lands the next photograph pre-zoomed into a corner of
      // an image the viewer has never seen.
      reset();
    },
    [photos.length, reset],
  );

  // Focus moves into the viewer on open so keys arrive without a click, and the
  // browser's own focus ring is left alone — this is a modal surface, not part
  // of the spatial grid.
  useEffect(() => {
    closeRef.current?.focus();
  }, []);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      switch (e.key) {
        case "ArrowRight":
          e.preventDefault();
          go(1);
          break;
        case "ArrowLeft":
          e.preventDefault();
          go(-1);
          break;
        case "0":
          reset();
          break;
        case "+":
        case "=":
          setZoom((z) => Math.min(MAX_ZOOM, z * 1.25));
          break;
        case "-":
          setZoom((z) => Math.max(1, z / 1.25));
          break;
        case " ":
          e.preventDefault();
          setPlaying((p) => !p);
          break;
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [go, reset]);

  // The slideshow never starts on its own, and never runs under reduced motion.
  // It is the one thing here that moves without being asked, so it waits to be
  // asked.
  useEffect(() => {
    if (!playing) return;
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
      setPlaying(false);
      return;
    }
    const t = setInterval(() => go(1), SLIDESHOW_MS);
    return () => clearInterval(t);
  }, [playing, go]);

  if (!item) return null;

  const onWheel = (e: React.WheelEvent) => {
    const next = Math.min(
      MAX_ZOOM,
      Math.max(1, zoom * (e.deltaY < 0 ? 1.15 : 1 / 1.15)),
    );
    setZoom(next);
    if (next === 1) setPan({ x: 0, y: 0 });
  };

  const onPointerDown = (e: React.PointerEvent) => {
    if (zoom === 1) return;
    (e.target as Element).setPointerCapture?.(e.pointerId);
    drag.current = { x: e.clientX, y: e.clientY, px: pan.x, py: pan.y };
  };

  const onPointerMove = (e: React.PointerEvent) => {
    const d = drag.current;
    if (!d) return;
    setPan({ x: d.px + (e.clientX - d.x), y: d.py + (e.clientY - d.y) });
  };

  const endDrag = () => {
    drag.current = null;
  };

  return (
    <div
      className="pview"
      role="dialog"
      aria-modal="true"
      aria-label={item.title}
      onClick={onClose}
    >
      <div
        className="pview__stage"
        onClick={(e) => e.stopPropagation()}
        onWheel={onWheel}
        onDoubleClick={reset}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={endDrag}
        onPointerCancel={endDrag}
      >
        {/* Full resolution, from the library rather than the cache — this is the
            one place the whole picture is wanted, and the endpoint hands back
            the rendition instead when the browser cannot decode the format. */}
        <img
          className="pview__img"
          src={`/api/items/${item.id}/photo`}
          alt={item.title}
          draggable={false}
          style={{
            transform: `translate3d(${pan.x}px, ${pan.y}px, 0) scale(${zoom})`,
            cursor: zoom > 1 ? (drag.current ? "grabbing" : "grab") : "default",
          }}
        />
      </div>

      {photos.length > 1 && (
        <>
          <button
            className="pview__nav pview__nav--prev"
            onClick={(e) => {
              e.stopPropagation();
              go(-1);
            }}
            aria-label="Previous picture"
          >
            ‹
          </button>
          <button
            className="pview__nav pview__nav--next"
            onClick={(e) => {
              e.stopPropagation();
              go(1);
            }}
            aria-label="Next picture"
          >
            ›
          </button>
        </>
      )}

      <div className="pview__bar" onClick={(e) => e.stopPropagation()}>
        <span className="pview__count">
          {index + 1} of {photos.length}
        </span>
        <span className="pview__name">{item.title}</span>
        <div className="pview__tools">
          {photos.length > 1 && (
            <button
              className="pview__btn"
              onClick={() => setPlaying((p) => !p)}
              aria-label={playing ? "Pause slideshow" : "Play slideshow"}
            >
              {playing ? "Pause" : "Slideshow"}
            </button>
          )}
          {zoom > 1 && (
            <button
              className="pview__btn"
              onClick={reset}
              aria-label="Reset zoom"
            >
              Reset
            </button>
          )}
          <button
            ref={closeRef}
            className="pview__btn"
            onClick={onClose}
            aria-label="Close viewer"
          >
            Close
          </button>
        </div>
      </div>
    </div>
  );
}
