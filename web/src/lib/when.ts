/*
 * A time, said the way a person reads it: "today 03:00", "yesterday 21:14",
 * or a date. Built from local components on purpose — the server sends UTC,
 * and a date taken from the ISO string is tomorrow every evening in the US.
 */
export function when(iso: string, now = new Date()): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  // A run that has only just happened says so: "Today 20:20" beside a button
  // pressed at 20:20 does not read as an answer to the press.
  const ago = now.getTime() - d.getTime();
  if (ago >= 0 && ago < 60_000) return "Just now";
  const time = d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
  const day = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const diff = Math.round((day(d) - day(now)) / 86_400_000);
  if (diff === 0) return `Today ${time}`;
  if (diff === -1) return `Yesterday ${time}`;
  if (diff === 1) return `Tomorrow ${time}`;
  return `${d.toLocaleDateString(undefined, { day: "numeric", month: "short" })} ${time}`;
}
