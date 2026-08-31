import { useEffect } from "react";

/*
 * Screens that own the picture turn the stars off.
 *
 * The star field is rendered once, globally, for every route — which is right
 * for a chrome that should feel like one continuous sky, and wrong for the one
 * screen whose whole job is a full-bleed backdrop. A detail page's fanart is
 * the best-looking thing in LANcast, and a texture drifting over it is a
 * texture competing with it.
 *
 * The nebula stays. It is what the backdrop is tinted *into* (see the artwork
 * rule in docs/design.md) and removing it would leave the artwork sitting on a
 * flat floor with the identity stopping at its edge. It is only the stars that
 * are removed, because they are the layer that reads as being in front.
 *
 * An attribute on the document element rather than a prop, because the element
 * being suppressed is not this screen's child — it belongs to main.tsx and sits
 * behind the whole app. The cleanup is what makes it safe: leaving the flag set
 * would take the stars away from every screen visited afterwards, which is the
 * failure this would have if it were written as a one-way switch.
 */
export const STARLESS_ATTR = "data-starless";

export function useStarless(active = true) {
  useEffect(() => {
    if (!active) return;
    const root = document.documentElement;
    root.setAttribute(STARLESS_ATTR, "");
    return () => root.removeAttribute(STARLESS_ATTR);
  }, [active]);
}
