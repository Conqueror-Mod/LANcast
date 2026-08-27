/*
 * Which way a shelf can still be scrolled.
 *
 * A pure function over three numbers, because the alternative is reading
 * `scrollLeft` inside a component and having nothing to assert: jsdom performs
 * no layout, so every width is zero and a test of the wiring would pass
 * whatever the arithmetic said. The rule is the part that can be wrong, so the
 * rule is the part that is separable.
 */

/**
 * Slack, in pixels, before an edge counts as reached.
 *
 * Fractional scroll positions are ordinary — a device-pixel-ratio of 1.25, a
 * smooth-scroll that lands at 1487.5 — and `scrollLeft + clientWidth` lands a
 * fraction short of `scrollWidth` at the true end. Without slack the right
 * chevron stays lit at the end of every shelf, pointing at nothing, which is
 * worse than no chevron because it invites a click that does nothing.
 */
export const EDGE_SLACK = 2;

export type Edges = { left: boolean; right: boolean };

/**
 * edges reports whether there is anything further to scroll to, each way.
 *
 * `scrollWidth <= clientWidth` is the common case on a wide window with a short
 * shelf, and it answers false to both — which is what hides the controls
 * entirely rather than showing two dead buttons.
 */
export function edges(
  scrollLeft: number,
  clientWidth: number,
  scrollWidth: number,
): Edges {
  if (scrollWidth <= clientWidth + EDGE_SLACK) return { left: false, right: false };
  return {
    left: scrollLeft > EDGE_SLACK,
    right: scrollLeft + clientWidth < scrollWidth - EDGE_SLACK,
  };
}

/**
 * pageBy is how far one press moves, in pixels.
 *
 * Nine tenths of the visible width rather than all of it. A full page leaves
 * nothing from the previous view on screen, so there is no landmark to say
 * which way you went or how far — the row simply becomes different tiles. The
 * overlap is small enough to feel like a page and large enough to keep one
 * tile's worth of context.
 */
export function pageBy(clientWidth: number, dir: 1 | -1): number {
  return Math.round(clientWidth * 0.9) * dir;
}
