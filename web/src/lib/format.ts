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
