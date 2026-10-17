import { ApiFailure } from "@/api/client";

/*
 * What to put on screen when something failed.
 *
 * Lived inside Settings.tsx until a second screen needed it. Shared rather
 * than copied: two versions of this drift, and the way they drift is that one
 * of them stops showing the server's own sentence and falls back to the
 * generic one — which is the difference between "that invite is for this
 * server" and "Something went wrong."
 */
export function errorMessage(err: unknown): string {
  if (err instanceof ApiFailure) return err.message;
  if (err instanceof Error) return err.message;
  return "Something went wrong.";
}

/*
 * A server's reason as a sentence. Reasons are written as phrases ("the
 * certificate expired on …") so they read inside a longer message, and they
 * are shown on their own in a few places; this capitalises and ends one.
 */
export function asSentence(reason: string): string {
  const r = reason.trim();
  if (!r) return r;
  const s = r[0].toUpperCase() + r.slice(1);
  return /[.!?]$/.test(s) ? s : s + ".";
}
