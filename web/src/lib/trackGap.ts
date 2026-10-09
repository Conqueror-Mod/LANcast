/*
 * How long the silence between two tracks is, and where it goes.
 *
 * Built before any gapless work (docs: measure first). The gap is the time
 * from one track's `ended` to the next track's `playing`, split at the next
 * source's `loadstart`:
 *
 *   - **to source**: our own work — advancing the queue, rendering, fetching
 *     the item and asking the server how to play it, then setting `src`.
 *   - **load**: the browser opening and buffering the new file.
 *
 * Which half dominates decides the fix. A gap that is mostly "to source" can
 * shrink by asking those questions early; one that is mostly "load" needs the
 * next track already loaded in a second element. Pure, so the arithmetic is
 * tested without a media element.
 */
export interface TrackGap {
  total: number;
  toSource: number;
  load: number;
}

export class TrackGapMeter {
  private endedAt: number | null = null;
  private sourceAt: number | null = null;

  ended(at: number): void {
    this.endedAt = at;
    this.sourceAt = null;
  }

  loadstart(at: number): void {
    if (this.endedAt !== null && this.sourceAt === null) this.sourceAt = at;
  }

  /** The gap that just closed, or null when this `playing` follows no track end. */
  playing(at: number): TrackGap | null {
    if (this.endedAt === null) return null;
    const source = this.sourceAt ?? at;
    const gap = {
      total: Math.round(at - this.endedAt),
      toSource: Math.round(source - this.endedAt),
      load: Math.round(at - source),
    };
    this.endedAt = null;
    this.sourceAt = null;
    return gap;
  }

  /** Something other than the queue moved on (a pause, a stop): forget the end. */
  reset(): void {
    this.endedAt = null;
    this.sourceAt = null;
  }
}
