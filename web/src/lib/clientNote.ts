/*
 * Write a line to the desktop window's log (docs/logging-plan.md, Phase 2).
 *
 * Things the page notices, a direct play that failed, a codec claim withdrawn,
 * being signed out, an uncaught error, used to go to a console nobody has open
 * and were gone by the time anybody asked. In the desktop client this sends
 * them to lancast-client.log through the lancastClientNote binding
 * (cmd/lancast/clientnote.go), which caps, cleans, deduplicates and
 * rate-limits them.
 *
 * In a browser tab there is no binding and this does nothing: a tab has no
 * log of its own, and a server endpoint for notes is deliberately not part of
 * this phase. It never throws and never awaits, because logging must never be
 * the thing that breaks the app.
 */

export type NoteArea = "playback" | "capabilities" | "auth" | "together" | "error";
export type NoteLevel = "info" | "warn" | "error";

declare global {
  interface Window {
    lancastClientNote?: (level: string, area: string, message: string) => Promise<boolean>;
  }
}

export function clientNote(level: NoteLevel, area: NoteArea, message: string): void {
  try {
    const p = window.lancastClientNote?.(level, area, message);
    if (p && typeof p.catch === "function") p.catch(() => {});
  } catch {
    // Nothing: a note that cannot be written is not worth an error.
  }
}

/** The first line of a stack that is not this file, for an uncaught error. */
export function whereFrom(err: unknown): string {
  const stack = err instanceof Error ? err.stack ?? "" : "";
  const frame = stack
    .split("\n")
    .map((l) => l.trim())
    .find((l) => l.startsWith("at "));
  return frame ? ` (${frame})` : "";
}
