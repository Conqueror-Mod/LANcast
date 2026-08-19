import { useDevice } from "./device";

/*
 * Spoilers on a season page.
 *
 * A still and a synopsis for an episode you have not reached is a spoiler, and
 * the season list is where it lands hardest: the next unwatched episode is the
 * one you are most likely to be looking at, and TMDB overviews are written as
 * summaries rather than as teases — "Leela discovers who her parents are" is the
 * plot, not an invitation.
 *
 * Doing nothing here is itself a choice, and the wrong one to make by accident
 * (season-page-plan.md §5).
 *
 * Per device rather than per account, following bigscreen: there is no per-user
 * preference store on the server, and inventing one for this would be a schema
 * decision made by a checkbox. The cost is honest and small — somebody who
 * watches on two machines sets it twice — and if per-user preferences ever
 * arrive, this is one of the settings that should move.
 */

export const SPOILERS_KEY = "lancast:spoilers";

/**
 * How much to hide on an episode nobody has started.
 *
 * - `show` — nothing hidden. For a rewatch, or a show whose plot is not the
 *   point.
 * - `synopsis` — the default. The still stays: a frame rarely gives a plot away,
 *   and it is what makes a row identifiable at a glance.
 * - `all` — the still goes too, replaced by the typographic state that already
 *   exists for episodes with no artwork. Nothing new to design, which is why
 *   this option is cheap enough to offer.
 */
export type SpoilerMode = "show" | "synopsis" | "all";

export const SPOILER_DEFAULT: SpoilerMode = "synopsis";

export function useSpoilerMode(): [SpoilerMode, (m: SpoilerMode) => void] {
  return useDevice<SpoilerMode>(SPOILERS_KEY, SPOILER_DEFAULT);
}

/**
 * What to hide for one episode.
 *
 * Pure, and the rule rather than the rendering — which is the half worth
 * testing, because every mistake in it is a spoiler shown or a season page
 * stripped of the information it exists to carry.
 */
export function spoilerState(
  mode: SpoilerMode,
  episode: { progress?: { position_ms: number; watched: boolean } | null },
): { hideSynopsis: boolean; hideStill: boolean } {
  const p = episode.progress;
  /*
   * Started counts as seen.
   *
   * Protection applies only to an episode with no progress at all. Somebody two
   * minutes into an episode has already met whatever the first scene gives
   * away, and hiding its synopsis from them is the setting getting in the way of
   * the person it is for — which is how a spoiler guard ends up switched off.
   */
  const seen = !!p && (p.watched || p.position_ms > 0);
  if (seen || mode === "show") {
    return { hideSynopsis: false, hideStill: false };
  }
  return { hideSynopsis: true, hideStill: mode === "all" };
}
