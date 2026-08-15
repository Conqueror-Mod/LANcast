import { useEffect } from "react";
import { readDevice, useDevice } from "./device";

/*
 * Bigscreen — the ten-foot version of the same client.
 *
 * Not a second UI. A separate television client is a second set of screens, a
 * second set of bugs, and a second place to forget the gold rule; every product
 * that has built one has spent the following two years explaining why a feature
 * exists in one and not the other. This is one attribute on the document root
 * and a handful of tokens under it: larger type, larger tiles, more space, and
 * a focus ring that reads from a sofa.
 *
 * That works because the keyboard model was built first (ADR 0004). A client
 * that could only be driven by a pointer would need a rewrite to be usable at
 * ten feet; this one already moves by arrow key, so making it *legible* at ten
 * feet is a styling question.
 *
 * The setting is per device — see device.ts — and applied before first paint by
 * a small script in index.html, because a television that flashes the desk
 * layout for one frame on every load is a television that looks broken.
 */

export const BIGSCREEN_KEY = "lancast:bigscreen";

export function bigscreenEnabled(): boolean {
  return readDevice<boolean>(BIGSCREEN_KEY, false);
}

/** Applies the current setting to the document root. Call once, in the shell. */
export function useBigscreen(): [boolean, (v: boolean) => void] {
  const [on, set] = useDevice<boolean>(BIGSCREEN_KEY, false);
  useEffect(() => {
    document.documentElement.toggleAttribute("data-bigscreen", on);
  }, [on]);
  return [on, set];
}

/*
 * The shortcut, and why it is not in the customizable map.
 *
 * The moment you want bigscreen is the moment you have just sat down ten feet
 * from the keyboard you were using — so it has to be reachable without reading
 * anything, and it has to be reachable *back*, because the way people discover
 * it is by turning it on and wanting out. A binding that can be rebound can be
 * rebound to nothing, and the one shortcut that must always work is the one
 * that undoes a mode you cannot read.
 */
export function useBigscreenShortcut(): void {
  const [on, set] = useBigscreen();
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const el = e.target as HTMLElement | null;
      if (
        el &&
        (el.tagName === "INPUT" ||
          el.tagName === "TEXTAREA" ||
          el.isContentEditable)
      ) {
        return;
      }
      // Ctrl+Shift+B: a plain letter would be stolen from the player, and the
      // player is where somebody sitting far away spends their time.
      if (e.ctrlKey && e.shiftKey && (e.key === "B" || e.key === "b")) {
        e.preventDefault();
        set(!on);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [on, set]);
}
