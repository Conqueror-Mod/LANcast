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

/*
 * A confidence score (0..1) as a whole-percent string, rounded **down**.
 *
 * Flooring is the whole point. The matcher auto-accepts at 0.85 and flags
 * anything below it as uncertain, so a score of 0.848 rendered with
 * Math.round reads "85%" — the threshold itself — on a row badged
 * "Uncertain". The number and the badge then contradict each other, and the
 * number is the one that looks authoritative.
 *
 * Flooring can only ever understate by less than a point, which is the safe
 * direction: it never claims a confidence the scorer did not have.
 */
export function scorePct(v: number | null | undefined): string {
  if (v == null || !isFinite(v)) return "—";
  return `${Math.floor(Math.max(0, Math.min(1, v)) * 100)}%`;
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
 * "S01E02" — but only for something that actually has episodes.
 *
 * A music track reuses the show columns: the scanner writes the album into
 * `series`, the disc into `season` and the track number into `episode`
 * (ADR 0024, and `ApplyTrackTags`). So "has a season and an episode" is true of
 * every tagged song in the library, and formatting on that test labelled Pearl
 * Jam's *Black* as **S00E33** and Garbage's *#1 Crush* as **S00E14** — disc
 * zero, track thirty-three.
 *
 * The kind is the only thing that separates them, which is why it is checked
 * here rather than left to each caller to remember.
 */
export function episodeCode(item: {
  kind?: string;
  season?: number | null;
  episode?: number | null;
}): string | null {
  if (item.kind !== "episode") return null;
  if (item.season == null || item.episode == null) return null;
  return `S${String(item.season).padStart(2, "0")}E${String(item.episode).padStart(2, "0")}`;
}

/*
 * The series and the episode together, for a tile that has one line to say what
 * it is looking at.
 *
 * Everywhere a tile appears outside its own show — Continue Watching, search, a
 * shelf — the episode title alone is not an identification. "Stray Dog Strut ·
 * 1998" reads as an obscure film; it is Cowboy Bebop S01E01.
 *
 * Returns null when the item is not an episode, which is how a caller decides
 * to fall back to the year.
 */
export function episodeLabel(item: {
  kind?: string;
  series?: string | null;
  season?: number | null;
  episode?: number | null;
}): string | null {
  const code = episodeCode(item);
  if (!code) return null;
  return item.series ? `${item.series} · ${code}` : code;
}
