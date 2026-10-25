import { apiGet } from "@/api/client";
import { fetchShowEpisodes } from "@/api/hooks";
import type { Item } from "@/api/types";

/*
 * What carries on after the thing being resumed.
 *
 * Reported: Continue Watching on a collection's next film, pressed from the
 * spotlight, played that one film and stopped. The franchise did not carry on
 * the way a show does, and nothing told it apart from a collection that had
 * actually ended. The same gap was in every resume path that handed the player
 * one id: an episode's own Continue, and a collection's Continue.
 *
 * One function, so the buttons cannot disagree about what follows what — the
 * rule showContinueTarget states for a show, extended to a franchise.
 *
 *   episode → the show's episodes from this one onward
 *   film in a collection → this film, then the collection's films after it in
 *     release order that have not been watched (skipping a watched one is the
 *     server's rule too: store.nextInSequence)
 *   anything else → no queue
 *
 * Never the earlier ones: Continue means "carry on from here", and queueing
 * what is behind would make the previous button walk back through it.
 *
 * A failed lookup answers "no queue" rather than throwing. One film is a worse
 * outcome than a franchise and a far better one than a button that does
 * nothing.
 */
export async function continueQueue(
  item: Pick<Item, "id" | "kind" | "continues_from">,
  /** For a film: the collection to carry on through, when the caller knows. */
  collectionID?: number,
): Promise<number[] | undefined> {
  try {
    if (item.kind === "episode") {
      // The endpoint resolves an episode to its show, so the client does not
      // walk the hierarchy.
      const eps = await fetchShowEpisodes(item.id);
      const from = eps.findIndex((e) => e.id === item.id);
      return from >= 0 && eps.length - from > 1 ? eps.slice(from).map((e) => e.id) : undefined;
    }
    if (item.kind === "movie") return await collectionQueue(item, collectionID);
  } catch {
    // Falls through to no queue.
  }
  return undefined;
}

async function collectionQueue(
  film: Pick<Item, "id" | "continues_from">,
  collectionID?: number,
): Promise<number[] | undefined> {
  const { items: cols } = await apiGet<{ items?: Item[] }>(`/api/items/${film.id}/collections`);
  if (!cols || cols.length === 0) return undefined;
  /*
   * The collection the caller or the server named, when either did: a film in
   * two collections (a franchise and a director's box set, say) continues the
   * one whose page or shelf row brought it here.
   */
  const named = collectionID ?? film.continues_from?.collection_id;
  const ordered = named ? [...cols].sort((a, b) => Number(b.id === named) - Number(a.id === named)) : cols;
  for (const col of ordered) {
    const { items: members } = await apiGet<{ items?: Item[] }>(`/api/items?collection_id=${col.id}`);
    const at = (members ?? []).findIndex((m) => m.id === film.id);
    if (at < 0) continue;
    const after = (members ?? []).slice(at + 1).filter((m) => !m.progress?.watched);
    if (after.length > 0) return [film.id, ...after.map((m) => m.id)];
  }
  return undefined;
}
