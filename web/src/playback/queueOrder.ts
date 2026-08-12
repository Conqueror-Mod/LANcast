/*
 * Queue ordering, as plain functions.
 *
 * This lived inside a useMemo and a useCallback in PlaybackProvider, which
 * meant the two things a queue actually has to get right — what order shuffle
 * produces, and which item comes next — could not be tested at all. The first
 * report of shuffle misbehaving (a video queue advancing 6, 2, 3, 4, 5 with the
 * button still lit) could not be confirmed or dismissed by reading the code,
 * because a correct shuffle can also produce a run that looks sequential.
 *
 * Out here they are ordinary functions over arrays, and the questions have
 * answers.
 */

/** Fisher–Yates, on a copy. The unbiased form: j is chosen from 0..i inclusive. */
export function shuffled(queue: number[], rand: () => number = Math.random): number[] {
  const out = queue.slice();
  for (let i = out.length - 1; i > 0; i--) {
    const j = Math.floor(rand() * (i + 1));
    [out[i], out[j]] = [out[j], out[i]];
  }
  return out;
}

/**
 * shuffledStartingWith shuffles the queue and puts `current` at the front.
 *
 * Without this, turning shuffle on leaves the playing track wherever the
 * shuffle happened to drop it — and playback continues *from there*, so
 * everything shuffled in front of it is never reached. On "shuffle the whole
 * music library" that was visible and awful: the current track landed at 691 of
 * 1,591, and with repeat off the session would end 900 tracks later having
 * skipped the first 690 entirely. The queue said 1,591; you were getting 900.
 *
 * Moving the current track to the front costs nothing — its position was
 * already arbitrary — and makes the count on the panel true.
 */
export function shuffledStartingWith(
  queue: number[],
  current: number,
  rand: () => number = Math.random,
): number[] {
  const rest = shuffled(queue.filter((id) => id !== current), rand);
  return queue.includes(current) ? [current, ...rest] : rest;
}

export type RepeatMode = "off" | "all" | "one";

/**
 * nextAfter returns the item that follows `current` in `order`, or null when
 * playback should stop.
 *
 * `repeat: "all"` wraps to the front. `"off"` stops at the end, which is what
 * finishing an album should do rather than starting it again. `"one"` never
 * reaches here — the caller reseeks instead of reloading the same source.
 */
export function nextAfter(
  order: number[],
  current: number,
  repeat: RepeatMode,
): number | null {
  const idx = order.indexOf(current);
  if (idx < 0) return null;
  if (idx + 1 < order.length) return order[idx + 1];
  if (repeat === "all" && order.length > 0) return order[0];
  return null;
}

/**
 * prevBefore is the mirror, without the wrap. Stepping back from the first item
 * does nothing rather than jumping to the end: "previous" at the start of a
 * record is not a request to hear the last song.
 */
export function prevBefore(order: number[], current: number): number | null {
  const idx = order.indexOf(current);
  if (idx <= 0) return null;
  return order[idx - 1];
}

/**
 * queueAfterEntry decides what the queue becomes when the player screen is
 * entered for `id` carrying `incoming`.
 *
 * Entering the player with no queue information — which is what returning from
 * the mini-player does, since it navigates to /watch/{id} with no ?queue= and
 * no state — used to replace the queue with a single item. The whole record
 * collapsed to the song that was playing: shuffle, repeat, the queue panel and
 * both skip buttons disappeared, and nothing could advance.
 *
 * play() already refused to restart the *item* on re-entry for exactly this
 * reason. The queue needed the same protection and did not have it.
 *
 * A genuine single-item play still replaces the queue: it is only "keep what we
 * have" when the caller supplied nothing AND the current queue already contains
 * what is being played.
 */
export function queueAfterEntry(
  existing: number[],
  incoming: number[],
  id: number,
): number[] {
  const noInformation = incoming.length === 1 && incoming[0] === id;
  if (noInformation && existing.includes(id)) return existing;
  return incoming;
}
