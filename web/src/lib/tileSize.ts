import { useDevice } from "./device";

/*
 * How large the tiles in a library grid are drawn.
 *
 * A device setting (device.ts), not a server one, for the same reason bigscreen
 * is: how big a poster wants to be is a fact about the screen you are looking
 * at, and syncing it would resize the phone in somebody's hand because the
 * television downstairs is a television.
 *
 * Discrete steps rather than a free pixel value. The grid is
 * `repeat(auto-fill, minmax(N, 1fr))`, so most of the pixels between two useful
 * column counts render identically — a continuous slider would spend two thirds
 * of its travel doing nothing visible, which reads as a broken control. Six
 * steps is enough that the smallest is a contact sheet and the largest is a
 * shelf of covers, and few enough that every notch changes the layout.
 */
export const TILE_SIZE_KEY = "lancast:tilesize";

/** Tile minimum widths, in px, smallest first. */
export const TILE_STEPS = [96, 124, 160, 200, 248, 306] as const;

/** The step the grid has always used, and what an unset preference means. */
export const TILE_DEFAULT_STEP = 2;

/**
 * A stored step, made safe to index with. A hand-edited localStorage value, a
 * preference written by a future build with more steps, or a NaN all resolve to
 * the default rather than to an undefined width — a grid whose `minmax` reads
 * `undefined` collapses to one column per item and looks like data loss.
 */
export function clampStep(step: unknown): number {
  if (typeof step !== "number" || !Number.isFinite(step)) return TILE_DEFAULT_STEP;
  return Math.min(TILE_STEPS.length - 1, Math.max(0, Math.round(step)));
}

/** The CSS width for a step. */
export function tileWidth(step: unknown): string {
  return `${TILE_STEPS[clampStep(step)]}px`;
}

/** The grid tile size as React state. Writes persist and update every reader. */
export function useTileSize(): [number, (step: number) => void] {
  const [raw, set] = useDevice<number>(TILE_SIZE_KEY, TILE_DEFAULT_STEP);
  return [clampStep(raw), (step: number) => set(clampStep(step))];
}
