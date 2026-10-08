/*
 * Where native video goes, from the page's own layout (ADR 0067, Phase 3).
 *
 * The desktop client has a window for the picture and does not know where the
 * player surface is; the page does. This turns the surface — full, docked or
 * idle — and the surface's box into what the client needs: a layout name and,
 * for the docked player, a rectangle in *physical* pixels, because the client
 * positions windows in device pixels and the page measures in CSS pixels.
 */
import type { Surface } from "./PlaybackProvider";

export interface NativeLayout {
  layout: "full" | "mini" | "pip" | "hidden";
  x: number;
  y: number;
  width: number;
  height: number;
}

export const HIDDEN: NativeLayout = { layout: "hidden", x: 0, y: 0, width: 0, height: 0 };

/*
 * `overGame`: a game screen is up (ADR 0076). The docked picture is then
 * "pip" — floated above the page, which stays over the game — rather than
 * "mini", above the main window. The rectangle is the same docked box either
 * way; only what it floats above differs.
 */
export function nativeLayout(
  surface: Surface,
  playingNatively: boolean,
  box: { left: number; top: number; width: number; height: number } | null,
  devicePixelRatio: number,
  overGame = false,
): NativeLayout {
  if (!playingNatively || surface === "idle") return HIDDEN;
  if (surface === "full") return { layout: "full", x: 0, y: 0, width: 0, height: 0 };
  if (!box || box.width <= 0 || box.height <= 0) return HIDDEN;
  const r = devicePixelRatio > 0 ? devicePixelRatio : 1;
  // Outer edges rounded outward so the picture never leaves a sliver of the
  // docked box's black showing along one side.
  const x = Math.floor(box.left * r);
  const y = Math.floor(box.top * r);
  return {
    layout: overGame ? "pip" : "mini",
    x,
    y,
    width: Math.ceil((box.left + box.width) * r) - x,
    height: Math.ceil((box.top + box.height) * r) - y,
  };
}

export function sameLayout(a: NativeLayout, b: NativeLayout): boolean {
  return (
    a.layout === b.layout &&
    a.x === b.x &&
    a.y === b.y &&
    a.width === b.width &&
    a.height === b.height
  );
}
