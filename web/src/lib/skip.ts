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
 * # Credits only from the gated rule
 *
 * The first credits rule was checked against forty films by looking at frames
 * around each marker, and about one in five was still in the film — a scene,
 * mid-dialogue, with ten minutes to run. A button that drops somebody out of
 * the third act one time in five is worse than no button, so for a while there
 * was none.
 *
 * The server now reads frames after each candidate and accepts it only if they
 * look like text on black (ADR 0054, 2026-10-03 amendment). On forty films it
 * had never seen, that gave no early answers at all. Its markers carry the
 * source `blackdetect-gated`, and that is the only source offered: a library
 * part-way through re-examination still holds markers from the old rule, and
 * the old rule is the one that was wrong.
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
  kind: "intro" | "credits";
  /** The position to seek to, in seconds. */
  atSeconds: number;
}

/** The credits source a skip is offered from on a film. */
export const GATED_CREDITS = "blackdetect-gated";

/*
 * The ungated rule's source, trusted on an episode and never on a film.
 *
 * Checked by eye, the ungated rule was early on one film in five and on one
 * episode in thirty-three: television fades into its credits, film often fades
 * inside its last act. The server no longer gates episodes for that reason,
 * because the film gate threw away more right answers on television than it
 * saved (ADR 0054, episode amendment).
 */
export const UNGATED_CREDITS = "blackdetect";

/*
 * Where a credits skip lands: this far before the end, not on it.
 *
 * Not the end itself, because the end is where the player's own `ended`
 * handling takes over — recording the film as watched and rolling on to the
 * next item — and that wants to be reached by *playing*, the way every other
 * ending is. A seek to the exact end asks a transcode to start with nothing
 * left to encode. A few seconds of the last card is the price of the ordinary
 * path.
 */
export const CREDITS_LEAD_SECONDS = 3;

/*
 * The window a credits marker must sit in to be offered, as shares of the file.
 *
 * The server never writes one outside 88–99.5%, and still four were found
 * outside it — chosen against a length the file did not have. A marker that
 * says the credits start at 73% is not believed here whatever the server says.
 * The top is 99% rather than 99.5%: in the last hundredth of a film there is
 * nothing worth a button.
 */
const CREDITS_FROM = 0.88;
const CREDITS_UNTIL = 0.99;

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
  durationSeconds = 0,
  itemKind?: string,
): SkipTarget | null {
  if (!markers || !Number.isFinite(positionSeconds)) return null;

  for (const m of markers) {
    if (m.kind === "credits") {
      const credits = creditsSkip(m, positionSeconds, durationSeconds, itemKind);
      if (credits) return credits;
      continue;
    }
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

/*
 * A credits skip, offered from the marker until a few seconds from the end.
 *
 * A visible button for the whole roll, never a jump: a person who stays for
 * the credits — or for what comes after them — has only to not press it.
 * Without a known duration there is no end to skip to and no window to check
 * the marker against, so there is no offer.
 */
function creditsSkip(
  m: Marker,
  positionSeconds: number,
  durationSeconds: number,
  itemKind: string | undefined,
): SkipTarget | null {
  const trusted =
    m.source === GATED_CREDITS ||
    (m.source === UNGATED_CREDITS && itemKind === "episode");
  if (!trusted) return null;
  if (!Number.isFinite(durationSeconds) || durationSeconds <= 0) return null;
  const start = m.start_ms / 1000;
  const share = start / durationSeconds;
  if (share < CREDITS_FROM || share >= CREDITS_UNTIL) return null;
  const to = durationSeconds - CREDITS_LEAD_SECONDS;
  if (positionSeconds >= start && positionSeconds < to) {
    return { kind: "credits", atSeconds: to };
  }
  return null;
}
