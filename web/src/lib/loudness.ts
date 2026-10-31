import type { components } from "@/api/schema";

type Loudness = components["schemas"]["Loudness"];

/** How music is levelled on this device: not at all, track by track, or by album. */
export type LevelMode = "off" | "track" | "album";

export const LEVEL_MODES: { id: LevelMode; label: string }[] = [
  { id: "off", label: "Off" },
  { id: "track", label: "By track" },
  { id: "album", label: "By album" },
];

/*
 * The volume factor a track's ReplayGain asks for (ADR 0108).
 *
 * By album keeps an album's own dynamics, a quiet song staying quieter than
 * the loud one after it, and falls back to the track's gain where the album
 * was never measured; by track evens out everything, the right choice for a
 * shuffled mix. Either way the peak caps the factor, so a gain can never push
 * a sample past full scale.
 *
 * Only ever down. A media element's volume stops at 1, so a track quieter
 * than the reference plays as it is rather than raised: levelling brings the
 * loud records down to meet it. An untagged track also plays as it is.
 */
export function levelGain(l: Loudness | null | undefined, mode: LevelMode): number {
  if (mode === "off" || !l) return 1;
  const gain = mode === "album" ? (l.album_gain_db ?? l.track_gain_db) : (l.track_gain_db ?? l.album_gain_db);
  if (gain == null || !Number.isFinite(gain)) return 1;
  const peak =
    mode === "album" && l.album_gain_db != null ? (l.album_peak ?? l.track_peak) : (l.track_peak ?? l.album_peak);
  let f = Math.pow(10, gain / 20);
  if (peak != null && peak > 0) f = Math.min(f, 1 / peak);
  return Math.min(1, Math.max(0, f));
}
