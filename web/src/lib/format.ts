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
