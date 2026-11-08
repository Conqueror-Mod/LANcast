import type { CSSProperties } from "react";
import type { ArtworkTone } from "@/api/types";

/*
 * The spotlight's tint, weighted by its picture (ADR 0118 §5, amending ADR
 * 0027 rule 3).
 *
 * The tint is the field's own wisps -- violet and blue above, teal and the
 * warm ember below -- and the picture only decides how strongly each shows.
 * No colour is introduced, nothing moves toward gold, and the strengths are
 * bounded so the base stays deep-space whatever the film looks like.
 *
 * Without a tone (loading, a grey picture, a covered one) the variables are
 * left unset and the stylesheet's defaults reproduce the tint as it was.
 */
export function toneStyle(tone: ArtworkTone | undefined): CSSProperties | undefined {
  if (!tone) return undefined;
  const sum = tone.violet + tone.blue + tone.teal + tone.ember;
  if (sum <= 0) return undefined;
  const clamp = (v: number) => Math.max(0, Math.min(1, v));
  return {
    "--tone-violet": clamp(tone.violet).toFixed(2),
    "--tone-blue": clamp(tone.blue).toFixed(2),
    "--tone-teal": clamp(tone.teal).toFixed(2),
    "--tone-ember": clamp(tone.ember).toFixed(2),
  } as CSSProperties;
}
