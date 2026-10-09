/*
 * Where the docked player sits, and how big it is (ADR 0076, stage 2).
 *
 * Chris's choices, 2026-10-08: drag the card and it settles into the nearest
 * of the four corners when let go, so it never ends up half over the middle of
 * a game; and three sizes, cycled. Both remembered on this computer — a
 * preference about a screen, not about an account.
 *
 * One small store rather than component state, because three things read it
 * and none of them is the parent of another: the picture (PlaybackProvider),
 * the control strip (MiniPlayer), and the native-video layout, which has to
 * tell the desktop client where the picture went. The drag offset lives here
 * too, unsaved, so the native picture can follow the card while it moves.
 */
import { useSyncExternalStore } from "react";

export type Corner = "br" | "bl" | "tl" | "tr";
export type DockSize = "s" | "m" | "l";

export interface DockState {
  corner: Corner;
  size: DockSize;
  /** While dragging: how far the card has moved, in CSS pixels. */
  dx: number;
  dy: number;
}

const KEY = "lancast:dock";
const DEFAULT: DockState = { corner: "br", size: "m", dx: 0, dy: 0 };

// Widths of the picture: a film's 16:9 box, and a record's square cover. The
// middle size is the one the corner has always had.
export const DOCK_WIDTHS: Record<DockSize, { video: number; audio: number }> = {
  s: { video: 220, audio: 100 },
  m: { video: 300, audio: 132 },
  l: { video: 420, audio: 180 },
};

export const CORNER_LABEL: Record<Corner, string> = {
  br: "Bottom right",
  bl: "Bottom left",
  tl: "Top left",
  tr: "Top right",
};

export const SIZE_LABEL: Record<DockSize, string> = { s: "Small", m: "Medium", l: "Large" };

const CORNERS: Corner[] = ["br", "bl", "tl", "tr"];
const SIZES: DockSize[] = ["s", "m", "l"];

// Round the screen, for the keyboard and the pad: the drag's snap without the
// drag.
export function nextCorner(c: Corner): Corner {
  return CORNERS[(CORNERS.indexOf(c) + 1) % CORNERS.length];
}

export function nextSize(s: DockSize): DockSize {
  return SIZES[(SIZES.indexOf(s) + 1) % SIZES.length];
}

/** The corner a card let go at (x, y) settles into: the quadrant it is in. */
export function nearestCorner(x: number, y: number, width: number, height: number): Corner {
  const left = x < width / 2;
  const top = y < height / 2;
  return top ? (left ? "tl" : "tr") : left ? "bl" : "br";
}

function load(): DockState {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return DEFAULT;
    const v = JSON.parse(raw) as Partial<DockState>;
    return {
      corner: CORNERS.includes(v.corner as Corner) ? (v.corner as Corner) : DEFAULT.corner,
      size: SIZES.includes(v.size as DockSize) ? (v.size as DockSize) : DEFAULT.size,
      dx: 0,
      dy: 0,
    };
  } catch {
    // Storage blocked or garbled: the corner it has always had.
    return DEFAULT;
  }
}

let state: DockState = load();
const listeners = new Set<() => void>();

export function getDock(): DockState {
  return state;
}

export function setDock(next: Partial<DockState>): void {
  state = { ...state, ...next };
  if ("corner" in next || "size" in next) {
    try {
      localStorage.setItem(KEY, JSON.stringify({ corner: state.corner, size: state.size }));
    } catch {
      // Remembered for this session only; nothing else depends on it.
    }
  }
  for (const l of listeners) l();
}

export function subscribeDock(fn: () => void): () => void {
  listeners.add(fn);
  return () => {
    listeners.delete(fn);
  };
}

export function useDock(): DockState {
  return useSyncExternalStore(subscribeDock, getDock, getDock);
}

/** For tests, which share one module. */
export function resetDock(): void {
  state = load();
  for (const l of listeners) l();
}
