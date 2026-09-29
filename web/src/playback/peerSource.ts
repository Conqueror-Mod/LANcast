import type { FilePath } from "./fileTransport";

/*
 * Where a peer's film comes from
 * ([ADR 0071](../../../docs/adr/0071-a-shared-library-is-a-standing-grant.md)
 * §5, and its amendment).
 *
 * Every URL here is **same-origin**, which is the whole shape of the feature
 * rather than an implementation detail: this client cannot reach another
 * household's server at all, because a window pins one server's key (ADR
 * 0070). So it asks its own server, which asks theirs.
 *
 * # Why these are their own functions and not a parameter on `sourceURL`
 *
 * `sourceURL` builds `/api/stream/{id}/…`, and an id from another server names
 * a **different item here**. Teaching it an optional peer would make every
 * existing call site one forgotten argument away from playing the wrong film
 * out of our own library — and that mistake has no error to report, because
 * the id it lands on is real.
 *
 * §5 makes the same argument about the browse screens: the surest way to keep
 * two things from merging is for them never to share a code path that could be
 * taught to concatenate. A separate function has no argument to forget.
 *
 * Pure, and takes everything it needs, so the choice of route is tested
 * without a server, a peer, or a media element.
 */

/** The prefix every peer route hangs off. */
function base(fingerprint: string): string {
  return `/api/peers/${encodeURIComponent(fingerprint)}`;
}

/*
 * peerSourceURL is the one the element is given.
 *
 * `path` comes from the same `filePath` the local player uses, so a device
 * that takes the HLS route for its own library takes it for a friend's too.
 * Nothing about the *decision* is made here — the far server made it, because
 * it holds the file and probed it.
 */
export function peerSourceURL(
  fingerprint: string,
  item: number,
  path: FilePath,
  offset = 0,
): string {
  const b = base(fingerprint);
  /*
   * `t` is where the far server starts converting, and it is the whole of how
   * a transcode seeks.
   *
   * A converted stream has no length and cannot be range-served — the bytes do
   * not exist until ffmpeg makes them — so moving the scrubber is not a seek
   * within a response, it is **a new session starting somewhere else**. Every
   * such session begins at zero, which is why the screen has to keep the offset
   * and add it back.
   *
   * Direct play takes none of this: those are the file's own bytes over a range
   * server, and the element seeks them itself.
   */
  const t = offset > 0 ? `t=${Math.floor(offset)}` : "";
  switch (path) {
    case "direct":
      return `${b}/stream?item=${item}`;
    case "hls":
      /*
       * The item is in the path, not the query, and it has to be: a playlist
       * names its segments with a prefix and cannot carry one. That shape
       * survives both hops — our server proxies it to theirs unchanged, and
       * rewrites the segment URLs in the playlist that comes back so they
       * point here rather than at a federation route this client cannot use.
       */
      return t === "" ? `${b}/hls/${item}/index.m3u8` : `${b}/hls/${item}/index.m3u8?${t}`;
    default:
      return `${b}/transcode?item=${item}` + (t === "" ? "" : `&${t}`);
  }
}

/** Where the list of a peer item's subtitle tracks lives. */
export function peerSubtitlesURL(fingerprint: string, item: number): string {
  return `${base(fingerprint)}/subtitles?item=${item}`;
}

/** One track, already converted to WebVTT by the server that holds it. */
export function peerSubtitleURL(
  fingerprint: string,
  item: number,
  key: string,
): string {
  return `${base(fingerprint)}/subtitles/${item}/${encodeURIComponent(key)}`;
}

/*
 * peerPlaybackURL asks how they would deliver it.
 *
 * Their answer, not ours. This household holds neither the file nor the probe,
 * so there is nothing here to decide with — and a guess made locally would be
 * a guess about somebody else's disk.
 */
export function peerPlaybackURL(fingerprint: string, item: number): string {
  return `${base(fingerprint)}/playback?item=${item}`;
}

/*
 * peerWatchingURL is the beat that says "still watching one of theirs"
 * ([ADR 0045](../../../docs/adr/0045-live-presence-between-paired-servers.md)
 * §10).
 *
 * **No title is sent, and that is the rule rather than an economy.** §3's
 * reductions — video only, the work and never the episode — are a function on
 * the server that owns the item, and it must stay one implementation. A client
 * that could name its own presence could name an episode, which §3 forbids by
 * name. So this says only *which item*, and the servers work out what may be
 * said about it.
 */
export function peerWatchingURL(fingerprint: string, item: number): string {
  return `${base(fingerprint)}/watching?item=${item}`;
}

/** What a peer says about one of their items: its name, and how long it is. */
export function peerItemURL(fingerprint: string, item: number): string {
  return `${base(fingerprint)}/item/${item}`;
}
