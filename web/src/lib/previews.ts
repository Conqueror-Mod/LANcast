/*
 * Scrub-bar previews (ADR 0105): which frame of which sheet shows a moment.
 *
 * Sheets are a grid of frames, one every `interval_ms`, left to right then
 * top to bottom. A moment past the last frame shows the last frame rather
 * than the black padding the final sheet ends in.
 */

export interface PreviewSheets {
  interval_ms: number;
  columns: number;
  rows: number;
  width: number;
  height: number;
  sheets: number;
}

export interface PreviewFrame {
  sheet: number;
  x: number;
  y: number;
}

export function frameAt(p: PreviewSheets, seconds: number, durationSec: number): PreviewFrame | null {
  if (p.interval_ms <= 0 || p.columns <= 0 || p.rows <= 0 || p.sheets <= 0) return null;
  const perSheet = p.columns * p.rows;
  // The last frame there is: the file's length in frames, and never past
  // the sheets that exist.
  const lastByDuration = durationSec > 0 ? Math.floor((durationSec * 1000 - 1) / p.interval_ms) : Infinity;
  const last = Math.min(p.sheets * perSheet - 1, lastByDuration);
  const index = Math.max(0, Math.min(last, Math.floor((seconds * 1000) / p.interval_ms)));
  const within = index % perSheet;
  return {
    sheet: Math.floor(index / perSheet),
    x: (within % p.columns) * p.width,
    y: Math.floor(within / p.columns) * p.height,
  };
}
