/*
 * The queue a season page hands the player: this season, then the rest of the
 * show.
 *
 * It used to be the season alone, so the last episode of a season ended the
 * night: no Up Next card, and auto play had nothing to roll on to. Ash vs Evil
 * Dead's first finale stopped dead that way (reported 2026-11-22), while the
 * show's own Continue already queued across seasons (lib/continueShow.ts).
 *
 * The show's list starts at this season's first episode, so Previous within the
 * season behaves as it did and never walks back into an earlier one. Until the
 * show's list arrives, or if it does not hold this season's episodes in order
 * (a filter the two endpoints disagree on), the season alone is the answer:
 * a shorter queue is better than one that skips an episode on this page.
 */
export function seasonQueue(season: number[], show: number[] | undefined): number[] {
  if (!show || season.length === 0) return season;
  const from = show.indexOf(season[0]);
  if (from < 0) return season;
  const onward = show.slice(from);
  for (let i = 0; i < season.length; i++) {
    if (onward[i] !== season[i]) return season;
  }
  return onward;
}
