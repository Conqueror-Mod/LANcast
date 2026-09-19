/*
 * The player backend: what PlaybackProvider needs from the thing that plays.
 *
 * ADR 0067, Phase 1. Until now that thing was always a `<video>` element, and
 * the provider said so everywhere. The desktop client is going to play through
 * libmpv, which is not an element, so the provider has to talk to a contract
 * instead of a tag.
 *
 * The contract is *the subset of HTMLMediaElement the provider already uses*,
 * with the same names, the same units and the same events. That is a
 * deliberate choice over designing a cleaner API:
 *
 *  - the `html5` backend is the element itself, with no adapter in between, so
 *    extracting the seam cannot change what a browser does;
 *  - every rule the provider has learned the hard way — resume after a reap,
 *    truncation recovery, the HLS verdict — is written against these events,
 *    and an mpv backend that raises the same events inherits all of it rather
 *    than re-specifying it.
 *
 * What is *not* here is as important. Text tracks, picture-in-picture, audio
 * sink selection and decode-quality counters are element features, and they
 * stay element features: the provider asks for `element()` and does without
 * when there is none. An mpv backend renders subtitles and picks the output
 * device itself, so pretending to be an element there would be a lie that
 * compiles.
 */

/** The events the provider listens for, by their media-element names. */
export const MEDIA_EVENTS = [
  "loadedmetadata",
  "loadeddata",
  "playing",
  "waiting",
  "error",
  "timeupdate",
  "play",
  "pause",
  "ended",
] as const;

export type MediaEventName = (typeof MEDIA_EVENTS)[number];

export interface MediaBackend extends EventTarget {
  /** Seconds into the current source. Writing it seeks. */
  currentTime: number;
  /** Seconds, NaN until known, Infinity for a source with no end. */
  readonly duration: number;
  readonly paused: boolean;
  /** 0..1 */
  volume: number;
  muted: boolean;
  playbackRate: number;
  /** The URL being played. Setting it does not load; `load()` does. */
  src: string;
  /** HAVE_* constants: 3 or more means frames are ready to play through. */
  readonly readyState: number;
  readonly videoWidth: number;
  readonly videoHeight: number;
  readonly error: MediaError | null;
  play(): Promise<void>;
  pause(): void;
  load(): void;
  /** Only "src" is meaningful: drop the source and release what it held. */
  removeAttribute(name: "src"): void;
}

// The html5 backend is the element. This line is the proof: if the element ever
// stops satisfying the contract, the build fails here rather than in a player.
const _elementIsABackend: (v: HTMLVideoElement) => MediaBackend = (v) => v;
void _elementIsABackend;

/**
 * mediaHandlers attaches one listener per media event to a backend, calling
 * through `handlers` at the time the event fires.
 *
 * Reading the handler table on each event, rather than binding it once, is
 * what makes this equivalent to React's `onTimeUpdate={…}` props: those see the
 * render's latest closure, and a listener bound at mount would see the first
 * render's for ever — a clock stuck on the first film's offset.
 *
 * Returns the detach function.
 */
export function attachMediaHandlers(
  target: MediaBackend,
  handlers: () => Partial<Record<MediaEventName, (media: MediaBackend) => void>>,
): () => void {
  const bound = MEDIA_EVENTS.map((name) => {
    const fn = () => handlers()[name]?.(target);
    target.addEventListener(name, fn);
    return [name, fn] as const;
  });
  return () => {
    for (const [name, fn] of bound) target.removeEventListener(name, fn);
  };
}
