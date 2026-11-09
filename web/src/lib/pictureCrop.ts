/*
 * The square crop in the picture editor, as numbers (ADR 0121).
 *
 * Kept apart from the component so the rules are tested without a canvas:
 * the image always covers the frame (no empty edge can be saved), zoom keeps
 * the point under the frame's centre where it was, and the saved region is
 * exactly what the frame showed.
 *
 * Units: `scale` is frame pixels per image pixel. `x` and `y` are where the
 * image's top-left corner sits in the frame, so both are zero or negative.
 */

export interface Crop {
  /** Image size in its own pixels. */
  w: number;
  h: number;
  /** The frame's side in screen pixels. */
  frame: number;
  zoom: number;
  x: number;
  y: number;
}

export const MAX_ZOOM = 4;

/** The scale at zoom 1: the short side exactly fills the frame. */
export function baseScale(c: Pick<Crop, "w" | "h" | "frame">): number {
  return c.frame / Math.min(c.w, c.h);
}

export function scaleOf(c: Crop): number {
  return baseScale(c) * c.zoom;
}

/** Keeps the image covering the frame. */
export function clamp(c: Crop): Crop {
  const zoom = Math.min(MAX_ZOOM, Math.max(1, c.zoom));
  const s = baseScale(c) * zoom;
  const minX = c.frame - c.w * s;
  const minY = c.frame - c.h * s;
  return {
    ...c,
    zoom,
    x: Math.min(0, Math.max(minX, c.x)),
    y: Math.min(0, Math.max(minY, c.y)),
  };
}

/** A new image, centred, at zoom 1. */
export function initialCrop(w: number, h: number, frame: number): Crop {
  const s = frame / Math.min(w, h);
  return clamp({ w, h, frame, zoom: 1, x: (frame - w * s) / 2, y: (frame - h * s) / 2 });
}

/** Moves the image by a screen distance. */
export function pan(c: Crop, dx: number, dy: number): Crop {
  return clamp({ ...c, x: c.x + dx, y: c.y + dy });
}

/** Changes zoom about the frame's centre. */
export function zoomTo(c: Crop, zoom: number): Crop {
  const before = scaleOf(c);
  const next = clamp({ ...c, zoom });
  const after = scaleOf(next);
  const mid = c.frame / 2;
  // The image point under the centre stays under the centre.
  const ix = (mid - c.x) / before;
  const iy = (mid - c.y) / before;
  return clamp({ ...next, x: mid - ix * after, y: mid - iy * after });
}

/** The region of the image the frame shows, in image pixels. */
export function sourceRect(c: Crop): { sx: number; sy: number; side: number } {
  const s = scaleOf(c);
  // `+ 0` turns -0 into 0, which a canvas does not mind and a test does.
  return { sx: -c.x / s + 0, sy: -c.y / s + 0, side: c.frame / s };
}
