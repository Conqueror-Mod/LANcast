import type { components } from "@/api/schema";

type Marker = components["schemas"]["Marker"];

/*
 * When to offer a skip, and to where.
 *
 * # Why this is a rule and not two lines in the player
 *
 * The decision is small and every part of it is a judgement that was measured,
 * so it is worth being able to state it in tests rather than infer it from a
 * JSX condition three levels inside an overlay.
 *
 * # Intros only, deliberately
 *
 * `credits` markers exist and are not offered. They were validated against forty
 * films by looking at a frame thirty seconds past each marker, and about one in
 * five was still in the film — a scene, mid-dialogue, with ten minutes to run.
 * A button that drops somebody out of the third act one time in five is worse
 * than no button, which is exactly what ADR 0054 gated against when it said the
 * rule was "consistent, not right".
 *
 * Intro markers come from a different detector — audio fingerprints compared
 * across a season, which finds the passage every episode shares — and every one
 * checked landed inside a title sequence.
 *
 * # The end is not padded
 *
 * On a long title sequence the detected end is occasionally a few seconds early,
 * so skipping lands in the last bar of the theme. Padding would trade that for
 * clipping the first line of the episode, and that is the worse half: nobody
 * minds the tail of a theme and everybody minds a missing first line.
 */

/** Where a skip would take you, and what it is skipping. */
export interface SkipTarget {
  kind: "intro";
  /** The position to seek to, in seconds. */
  atSeconds: number;
}

/**
 * skipTarget returns the skip to offer at this moment, or null.
 *
 * Offered only while the playhead is *inside* a marked range: a button that
 * appears before the intro has started is offering to skip something that is
 * not happening, and one that lingers after is offering to jump backwards.
 */
export function skipTarget(
  markers: Marker[] | undefined,
  positionSeconds: number,
): SkipTarget | null {
  if (!markers || !Number.isFinite(positionSeconds)) return null;

  for (const m of markers) {
    if (m.kind !== "intro") continue;
    /*
     * An intro with no end is not skippable, and this is not defensive
     * programming. Credits legitimately have no end — they run to the end of
     * the file — so the field is nullable for a real reason, and an intro
     * without one is a detector that found a beginning and could not say where
     * it stopped. There is nowhere to skip *to*.
     */
    if (m.end_ms == null) continue;

    const start = m.start_ms / 1000;
    const end = m.end_ms / 1000;
    if (end <= start) continue;

    if (positionSeconds >= start && positionSeconds < end) {
      return { kind: "intro", atSeconds: end };
    }
  }
  return null;
}
