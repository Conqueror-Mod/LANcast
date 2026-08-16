// Runtime as "2h 8m", from milliseconds. Empty string when unknown, so the
// caller can omit it cleanly.
export function runtime(ms: number | null | undefined): string {
  if (!ms || ms <= 0) return "";
  const mins = Math.round(ms / 60000);
  if (mins < 60) return `${mins}m`;
  return `${Math.floor(mins / 60)}h ${mins % 60}m`;
}

// A rating like 7.7 → "7.7". Null/zero yields empty.
export function rating(value: number | null | undefined): string {
  if (!value) return "";
  return value.toFixed(1);
}

// A human label for an external rating source (ADR 0019). The set is open, so an
// unknown source falls back to a tidied version of its own id rather than
// vanishing — a new source shows up labelled, just not prettily.
const RATING_LABELS: Record<string, string> = {
  imdb: "IMDb",
  rotten_tomatoes: "Rotten Tomatoes",
  metacritic: "Metacritic",
  tmdb: "TMDB",
};
export function ratingLabel(source: string): string {
  return (
    RATING_LABELS[source] ??
    source.replace(/_/g, " ").replace(/\b\w/g, (c) => c.toUpperCase())
  );
}

// Seconds → clock, "M:SS" under an hour and "H:MM:SS" over. For the player.
export function clock(totalSeconds: number): string {
  if (!isFinite(totalSeconds) || totalSeconds < 0) totalSeconds = 0;
  const s = Math.floor(totalSeconds % 60);
  const m = Math.floor((totalSeconds / 60) % 60);
  const h = Math.floor(totalSeconds / 3600);
  const ss = String(s).padStart(2, "0");
  if (h > 0) return `${h}:${String(m).padStart(2, "0")}:${ss}`;
  return `${m}:${ss}`;
}

/*
 * "S01E02", and the series it belongs to.
 *
 * An episode's own title is not enough to identify it anywhere it appears
 * outside its show. On Continue Watching, "Stray Dog Strut · 1998" reads as a
 * film nobody has heard of — it is the second episode of Cowboy Bebop, and the
 * tile said nothing that would tell you so.
 *
 * Detail.tsx already built this string twice, for a download filename and a
 * receipt. A third copy in the tile would be the version that drifts, so it
 * lives here and all three call it.
 *
 * Returns null when the item is not an episode, which is how a caller decides
 * to fall back to the year.
 */
export function episodeLabel(item: {
  series?: string | null;
  season?: number | null;
  episode?: number | null;
}): string | null {
  if (item.season == null || item.episode == null) return null;
  const code = `S${String(item.season).padStart(2, "0")}E${String(item.episode).padStart(2, "0")}`;
  return item.series ? `${item.series} · ${code}` : code;
}
