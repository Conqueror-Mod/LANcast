/*
 * "Play all" or "Play".
 *
 * Play all promises several things in a row. On an album of one track, a
 * season of one episode, a collection of one film or a library holding a single
 * title, there is nothing to be "all" of, and the button reads Play. Shuffle is
 * dropped there too: shuffling one thing is playing it.
 *
 * One rule for every place that offers it (a container's page, an artist's
 * page, a tile's menu, a library's bar), so they cannot disagree about the same
 * album. A count that is not known (a list that did not attach one) keeps the
 * old wording rather than guessing.
 */
export function playAllLabel(count: number | null | undefined): "Play" | "Play all" {
  return count === 1 ? "Play" : "Play all";
}

/** Whether to offer Shuffle beside it: not for a single item. */
export function offersShuffle(count: number | null | undefined): boolean {
  return count !== 1;
}
