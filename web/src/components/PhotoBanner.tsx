import { useEffect, useRef, useState } from "react";
import { artworkURL } from "@/api/client";
import { useFocusable } from "@/focus/FocusController";
import type { Item } from "@/api/types";
import "./PhotoBanner.css";

// How long each picture holds before the next one fades in. Slow on purpose:
// this is a banner someone glances at while deciding where to go, not a
// slideshow they are watching. Anything faster reads as a demo reel.
const HOLD_MS = 7000;

function useReducedMotion(): boolean {
  const [reduced, setReduced] = useState(
    () =>
      typeof window !== "undefined" &&
      window.matchMedia("(prefers-reduced-motion: reduce)").matches,
  );
  useEffect(() => {
    const mq = window.matchMedia("(prefers-reduced-motion: reduce)");
    const onChange = () => setReduced(mq.matches);
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, []);
  return reduced;
}

// A banner that cycles pictures, or shows the one that was chosen.
//
// The Home hero deliberately does not move (ADR 0027): it is a decision surface,
// and a moving target makes deciding harder. This one does, and the distinction
// is real rather than convenient — here the pictures *are* the content, and
// cycling through them is the point of the screen. It stops on hover and focus,
// stops entirely under reduced motion, and stops the moment a picture is
// selected, because at that point it has become a decision surface too.
export function PhotoBanner({
  photos,
  selected,
  onExpand,
  label,
}: {
  photos: Item[];
  selected?: Item | null;
  onExpand: (item: Item) => void;
  label?: string;
}) {
  const reduced = useReducedMotion();
  const [index, setIndex] = useState(0);
  const [paused, setPaused] = useState(false);

  // Reset when the set changes, so navigating between galleries does not land
  // mid-rotation on an index the new gallery may not even have.
  const key = photos.map((p) => p.id).join(",");
  const lastKey = useRef(key);
  if (lastKey.current !== key) {
    lastKey.current = key;
    if (index !== 0) setIndex(0);
  }

  const cycling = !selected && !reduced && !paused && photos.length > 1;
  useEffect(() => {
    if (!cycling) return;
    const t = setInterval(() => {
      setIndex((i) => (i + 1) % photos.length);
    }, HOLD_MS);
    return () => clearInterval(t);
  }, [cycling, photos.length]);

  const shown = selected ?? photos[Math.min(index, photos.length - 1)];
  const expand = useFocusable(() => shown && onExpand(shown));

  if (!shown) return null;

  // The banner reads the 1280px variant, not the original: it is a backdrop at
  // display size, and pulling a 24-megapixel file to fill it would make the
  // screen slow for no visible gain. Full resolution belongs to the viewer.
  const src = artworkURL(shown.artwork?.poster, "fanart");

  return (
    <section
      className="pbanner"
      aria-label={label ?? "Pictures"}
      onMouseEnter={() => setPaused(true)}
      onMouseLeave={() => setPaused(false)}
      onFocusCapture={() => setPaused(true)}
      onBlurCapture={() => setPaused(false)}
    >
      {/* Keyed by id so React swaps elements rather than mutating one src,
          which is what lets the CSS fade run at all. */}
      {src && (
        <img
          key={shown.id}
          className="pbanner__img"
          src={src}
          alt={shown.title}
          draggable={false}
        />
      )}
      <div className="pbanner__scrim" />

      <div className="pbanner__foot">
        <span className="pbanner__title">{selected ? shown.title : label}</span>
        <button
          {...expand}
          className="pbanner__expand"
          onClick={() => onExpand(shown)}
          aria-label={`View ${shown.title} full screen`}
        >
          Expand
        </button>
      </div>
    </section>
  );
}
