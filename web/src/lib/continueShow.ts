import { fetchShowContinue, fetchShowEpisodes } from "@/api/hooks";

/*
 * Where a show resumes, and the queue that carries it onward.
 *
 * One function because there are two buttons — the show page's Continue and a
 * Continue Watching tile — and a show cannot be allowed two answers to "what
 * comes next". The rule lives in NextEpisodeFor on the server; this is the one
 * client path that asks it.
 *
 * It re-asks on every press rather than trusting anything already on screen.
 * A shelf tile carries `next_episode` so it can draw a title and a resume bar,
 * and that payload is up to ten seconds old — fine for drawing, not for
 * deciding. The failure being designed against is precisely a stale read:
 * pressing continue and landing on an episode already finished. The server
 * sends no-store on that endpoint for the same reason.
 *
 * The queue is the episodes from this one onward, never the whole show.
 * Including the earlier ones would make the *previous* button walk backwards
 * through episodes already watched, which is what Play from the top is for.
 * Handing over no queue at all is worse and has happened: the episode plays,
 * the show stops dead, and with repeat on the same episode loops for ever
 * because a one-item queue wraps onto itself.
 */
export type ShowTarget =
  | { kind: "play"; episodeID: number; queue: number[] }
  | { kind: "exhausted" }
  | { kind: "empty" };

export async function showContinueTarget(showID: number): Promise<ShowTarget> {
  const next = await fetchShowContinue(showID);
  if (next.exhausted) return { kind: "exhausted" };
  if (!next.episode) return { kind: "empty" };

  const episode = next.episode;
  const eps = await fetchShowEpisodes(showID);
  const from = eps.findIndex((e) => e.id === episode.id);
  const queue = from >= 0 ? eps.slice(from) : [episode];
  return {
    kind: "play",
    episodeID: episode.id,
    queue: queue.map((e) => e.id),
  };
}
