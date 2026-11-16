import { apiSend } from "@/api/client";

/*
 * Telling the server this viewer has stopped watching an item (T-5).
 *
 * An HLS encode writes segments whether anybody reads them or not. Detaching
 * the element's source ends a progressive stream, because ffmpeg stalls on a
 * pipe nobody reads; it does nothing to an HLS encode, which ran on for up to
 * ten minutes after the viewer had gone (The Fifth Element, idle_seconds=614).
 *
 * The request is fire-and-forget, so it can race the next one. Leaving a film
 * and starting it again at once would let the new playlist request reach the
 * server first and the leave then stop the encode just started. The load waits
 * on leftHLS(id) for that reason: from this client, a leave always lands before
 * the same item is asked for again.
 */
const pending = new Map<number, Promise<void>>();

export function leaveHLS(id: number, keepalive = false): void {
  if (id <= 0) return;
  const p: Promise<void> = apiSend(
    `/api/stream/${id}/hls`,
    "DELETE",
    undefined,
    keepalive ? { keepalive: true } : undefined,
  )
    // Failing to say goodbye is not an error anybody can act on: the idle
    // timeout is still the backstop.
    .catch(() => {})
    .finally(() => {
      if (pending.get(id) === p) pending.delete(id);
    });
  pending.set(id, p);
}

// Resolves once any leave sent for this item has been answered.
export function leftHLS(id: number): Promise<void> {
  return pending.get(id) ?? Promise.resolve();
}
