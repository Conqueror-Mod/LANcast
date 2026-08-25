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

/*
 * Positions, not identities.
 *
 * Everything above answers "what comes after this id", which is the wrong
 * question the moment a queue can hold the same id twice — and a playlist can
 * (ADR 0030). indexOf finds the *first* occurrence, so a track appearing at
 * positions 0 and 2 always resumed from position 0: Road Trip played 1, 2, then
 * 1 again, and then 2 again, forever. Nothing else in the queue was reachable.
 *
 * The cursor is a position now. The id is still what plays; the position is
 * where in the order it is playing from, and only the position can tell two
 * copies of the same track apart.
 */

/**
 * resolvePos validates a remembered position against the order.
 *
 * The position can go stale — shuffle is toggled, the queue is replaced — and a
 * stale index into a reordered array is worse than no index, because it points
 * confidently at the wrong song. When it no longer holds the item that is
 * playing, fall back to finding that item. First occurrence is the honest
 * answer there: with nothing else to go on, the front of the queue is where a
 * listener would assume they are.
 */
export function resolvePos(order: number[], pos: number, current: number): number {
  if (pos >= 0 && pos < order.length && order[pos] === current) return pos;
  return order.indexOf(current);
}

/** The next position, or null when playback should stop. */
export function nextPos(
  order: number[],
  pos: number,
  repeat: RepeatMode,
): number | null {
  if (pos < 0) return null;
  if (pos + 1 < order.length) return pos + 1;
  if (repeat === "all" && order.length > 0) return 0;
  return null;
}

/**
 * The previous position, or null to stay where we are.
 *
 * "all" wraps to the end, matching next. Without repeat, previous at the front
 * does nothing rather than jumping to the last track — that is a restart, and
 * the caller handles it.
 */
export function prevPos(
  order: number[],
  pos: number,
  repeat: RepeatMode,
): number | null {
  if (pos < 0) return null;
  if (pos > 0) return pos - 1;
  if (repeat === "all" && order.length > 0) return order.length - 1;
  return null;
}

/**
 * startOf picks which id a freshly-built queue should begin on.
 *
 * "Randomize all" always started with the same film, and the reason is a
 * comment that had the mechanism exactly backwards. Every caller navigated to
 * `ids[0]` and left the randomising to shuffle, on the grounds that
 * `shuffledStartingWith` reorders everything anyway so a random start here
 * would be "a second randomiser doing nothing".
 *
 * It is the opposite. `shuffledStartingWith` *pins* the id it is given to the
 * front — deliberately, so that turning shuffle on mid-queue does not strand
 * everything shuffled ahead of the playing track. Hand it a fixed `ids[0]` and
 * it faithfully pins the same film every time: the shuffle is real, and it is a
 * shuffle of positions 2..n. The one position a listener actually notices is
 * the only one that never moved.
 *
 * So the random choice has to happen *here*, before the queue is handed over.
 * An ordered start still takes ids[0], which is what Play all means.
 */
export function startOf(
  ids: number[],
  shuffle: boolean,
  rand: () => number = Math.random,
): number | undefined {
  if (ids.length === 0) return undefined;
  if (!shuffle) return ids[0];
  return ids[Math.floor(rand() * ids.length)];
}

/**
 * shuffleForEntry decides what shuffle should be when the player is entered.
 *
 * Shuffle belongs to the session, deliberately: returning to the player from
 * the mini-player must not silently clear a shuffle you turned on, so an entry
 * carrying no queue information leaves the flag alone.
 *
 * That rule was applied to *every* entry, and it is wrong for half of them. A
 * caller that hands over a queue is making a statement about order — Continue
 * on a show says "these episodes, from here, in this sequence"; pressing an
 * episode row says "this season from here"; an album's track list says "the
 * record, in the order the record plays". None of them passed `shuffle`,
 * because none of them thought they were saying anything about it, so all of
 * them inherited whatever the session happened to hold.
 *
 * Which is how Futurama played out of order. Randomize all on a film library
 * turns shuffle on for the session; pressing Continue on a show afterwards
 * hands over a correctly ordered queue and plays it shuffled. Nothing about
 * the show, its data or its ordering is wrong — both the episode query and the
 * client's own walk return season 1 episode 1 first — and every screen still
 * *displays* the right order, because the shuffled order lives only in the
 * player. That is what makes it look like the queue is broken rather than the
 * flag.
 *
 * So: an explicit request always wins; supplying a queue without one means "in
 * this order"; supplying nothing keeps the session's flag.
 */
export function shuffleForEntry(
  explicit: boolean | undefined,
  suppliedQueue: boolean,
  current: boolean,
): boolean {
  if (explicit !== undefined) return explicit;
  return suppliedQueue ? false : current;
}
