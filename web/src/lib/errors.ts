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
