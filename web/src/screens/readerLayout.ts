/*
 * Two facing pages (settings-3-plan step 4).
 *
 * A printed comic opens with its cover alone on the right, and every spread
 * after that pairs an even page with the odd one after it. Counting from 0,
 * as the reader does: [0], [1, 2], [3, 4], … and a last page alone when the
 * count leaves one over. Pairing [0, 1] instead puts every two-page panel on
 * the wrong two sheets, which is the one mistake a spread view must not make.
 *
 * Left to right only. Reading direction is a property of the library and
 * comes with per-library options (step 6).
 *
 * Pure, so the pairing is tested without a reader on screen.
 */

/** The first page of the spread holding page `p`. */
export function spreadStart(p: number): number {
  if (p <= 0) return 0;
  return p % 2 === 1 ? p : p - 1;
}

/** The pages shown with page `p`, left to right. */
export function spreadPages(p: number, total: number): number[] {
  if (total <= 0) return [];
  const start = Math.min(spreadStart(p), spreadStart(total - 1));
  if (start === 0) return [0];
  return start + 1 < total ? [start, start + 1] : [start];
}

/** The first page of the next spread, or the same one at the end. */
export function nextSpread(p: number, total: number): number {
  const start = spreadStart(p);
  const next = start === 0 ? 1 : start + 2;
  return next < total ? next : start;
}

/** The first page of the previous spread, or 0 at the start. */
export function prevSpread(p: number): number {
  const start = spreadStart(p);
  if (start <= 1) return 0;
  return start - 2;
}
