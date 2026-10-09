import { MEDIA_EVENTS, type MediaBackend } from "./backend";

/*
 * The music deck: two audio elements behind one MediaBackend face, so the next
 * track is already loaded and already playing when this one ends
 * (docs/gapless-plan.md, step 1).
 *
 * # Why it has to start on its own
 *
 * Measured with #811: from one track's `ended` to the next track's `playing`
 * the provider spends 70–90 ms — advancing the queue, asking the server how to
 * play the next item, setting `src` — and an MP3 carries some tens of
 * milliseconds of encoder padding at each end on top. Nothing that waits for
 * `ended` can be gapless. So the deck is told what comes next, loads it on a
 * standby element, and starts it `overlap` seconds before the active track
 * ends: about 60 ms to cover the padding, or a crossfade's length.
 *
 * # Why the provider does not notice
 *
 * The provider's advance is unchanged. When the old track really ends the deck
 * fires `ended` as an element would, the provider saves progress and advances,
 * tears the old source down (pause, remove `src`, load) and, after asking the
 * server, sets `src` to the next track. The deck is **adopting** throughout:
 * the teardown is ignored, because the source it would tear down is already
 * the next track, and no event from the new element reaches the provider
 * until `src` is set to the very URL that is playing. Then the deck replays the
 * events of a source opening (`loadedmetadata` … `playing`), and from there it
 * is one element again.
 *
 * Anything else is a change of plan: a `src` for a different URL aborts the
 * handover and loads that instead, and an adoption nobody completes within
 * ADOPT_TIMEOUT_MS stops, so a queue that changed its mind cannot leave music
 * playing that no screen knows about.
 */

/** What the deck needs of an element; an HTMLAudioElement is one. */
export interface DeckElement extends EventTarget {
  src: string;
  currentTime: number;
  readonly duration: number;
  readonly paused: boolean;
  readonly ended: boolean;
  volume: number;
  muted: boolean;
  playbackRate: number;
  preload: string;
  readonly readyState: number;
  readonly error: MediaError | null;
  play(): Promise<void>;
  pause(): void;
  load(): void;
  removeAttribute(name: "src"): void;
}

export interface DeckClock {
  setTimeout(fn: () => void, ms: number): number;
  clearTimeout(id: number): void;
  setInterval(fn: () => void, ms: number): number;
  clearInterval(id: number): void;
}

const realClock: DeckClock = {
  setTimeout: (fn, ms) => window.setTimeout(fn, ms),
  clearTimeout: (id) => window.clearTimeout(id),
  setInterval: (fn, ms) => window.setInterval(fn, ms),
  clearInterval: (id) => window.clearInterval(id),
};

/** The overlap that hides MP3 encoder padding at a gapless join. */
export const GAPLESS_OVERLAP_S = 0.06;
/** How long before the end the next track is loaded. */
export const PRELOAD_LEAD_S = 20;
/** An adoption nobody completes is abandoned after this. */
export const ADOPT_TIMEOUT_MS = 3000;
// How often a crossfade's volumes are stepped.
const FADE_STEP_MS = 50;

// The events a source opening fires, replayed when the provider adopts the
// track that is already playing.
const OPENING = ["loadedmetadata", "durationchange", "loadeddata", "play", "playing", "timeupdate"] as const;

/** Equal-power curves: the sum of the squared gains stays at one. */
export function fadeOutGain(t: number): number {
  return Math.cos((Math.min(1, Math.max(0, t)) * Math.PI) / 2);
}
export function fadeInGain(t: number): number {
  return Math.sin((Math.min(1, Math.max(0, t)) * Math.PI) / 2);
}

type Queued = { url: string; overlap: number };

export class MusicDeck extends EventTarget implements MediaBackend {
  private active: DeckElement;
  private standby: DeckElement;
  private queued: Queued | null = null;
  private standbyLoaded = false;
  private started = false; // the standby has begun playing for a handover
  private startTimer: number | null = null;
  private fadeTimer: number | null = null;
  private adopting: string | null = null;
  private adoptTimer: number | null = null;
  private adoptSrcSet = false;
  private level = 1;
  private readonly clock: DeckClock;
  /** Called when the provider adopts a track that was already playing. */
  onAdopt?: (overlapS: number) => void;
  private adoptedOverlap = 0;

  constructor(a: DeckElement, b: DeckElement, clock: DeckClock = realClock) {
    super();
    this.active = a;
    this.standby = b;
    this.clock = clock;
    for (const el of [a, b]) {
      el.preload = "auto";
      // loadstart too: not a provider event, but the gap meter splits on it.
      for (const name of ["loadstart", ...MEDIA_EVENTS]) {
        el.addEventListener(name, () => this.fromElement(el, name));
      }
    }
  }

  // ---- what comes next ----------------------------------------------------

  /**
   * The track that follows, as a direct URL, and how far to overlap it:
   * GAPLESS_OVERLAP_S, or a crossfade's seconds. Calling it again with a
   * different URL replaces the plan; with null, cancels it.
   */
  queue(next: Queued | null): void {
    if (this.started || this.adopting) return; // a handover is under way
    if (next && this.queued && next.url === this.queued.url && next.overlap === this.queued.overlap) return;
    this.cancelQueued();
    if (!next) return;
    this.queued = next;
    this.standby.src = next.url;
    this.standby.load();
    this.standbyLoaded = true;
    this.scheduleStart();
  }

  /** Whether a handover is planned or under way (for tests and the provider). */
  get pending(): boolean {
    return this.queued !== null || this.adopting !== null;
  }

  private cancelQueued(): void {
    if (this.startTimer !== null) this.clock.clearTimeout(this.startTimer);
    this.startTimer = null;
    if (this.standbyLoaded && !this.started) {
      this.standby.removeAttribute("src");
      this.standby.load();
    }
    this.standbyLoaded = false;
    this.queued = null;
  }

  private remaining(): number {
    const d = this.active.duration;
    if (!Number.isFinite(d) || d <= 0) return Infinity;
    return d - this.active.currentTime;
  }

  // Called on every active timeupdate: the start is set by a timer for the
  // exact moment, because timeupdate itself fires only every ~250 ms.
  private scheduleStart(): void {
    if (!this.queued || this.started || this.active.paused) return;
    const lead = this.remaining() - this.queued.overlap;
    if (!Number.isFinite(lead) || lead > 1) return;
    if (this.startTimer !== null) this.clock.clearTimeout(this.startTimer);
    this.startTimer = this.clock.setTimeout(() => this.startStandby(), Math.max(0, lead * 1000));
  }

  private startStandby(): void {
    this.startTimer = null;
    if (!this.queued || this.started || this.active.paused) return;
    this.started = true;
    const overlap = this.queued.overlap;
    const s = this.standby;
    s.currentTime = 0;
    s.muted = this.active.muted;
    s.playbackRate = 1;
    s.volume = overlap > GAPLESS_OVERLAP_S ? 0 : this.level;
    void s.play().catch(() => {});
    if (overlap > GAPLESS_OVERLAP_S) this.crossfade(overlap);
  }

  private crossfade(seconds: number): void {
    const steps = Math.max(1, Math.round((seconds * 1000) / FADE_STEP_MS));
    let i = 0;
    const out = this.active;
    const into = this.standby;
    this.fadeTimer = this.clock.setInterval(() => {
      i++;
      const t = i / steps;
      out.volume = this.level * fadeOutGain(t);
      into.volume = this.level * fadeInGain(t);
      if (i >= steps && this.fadeTimer !== null) {
        this.clock.clearInterval(this.fadeTimer);
        this.fadeTimer = null;
      }
    }, FADE_STEP_MS);
  }

  // ---- events --------------------------------------------------------------

  private fromElement(el: DeckElement, name: string): void {
    if (el === this.active) {
      if (name === "timeupdate") this.scheduleStart();
      if (name === "ended" && this.started) return this.handOver();
      // While adopting, the active element is the next track and the provider
      // has not asked for it yet: nothing it says belongs to the old one.
      if (this.adopting) return;
      this.dispatchEvent(new Event(name));
      return;
    }
    // The standby is silent to the provider. A failure loading it just means
    // no gapless join this time: the ordinary advance will load it again.
    if (name === "error" && el === this.standby && !this.started) {
      this.cancelQueued();
    }
  }

  private handOver(): void {
    const q = this.queued;
    if (!q) return;
    if (this.fadeTimer !== null) this.clock.clearInterval(this.fadeTimer);
    this.fadeTimer = null;
    const old = this.active;
    this.active = this.standby;
    this.standby = old;
    this.active.volume = this.level;
    this.queued = null;
    this.started = false;
    this.standbyLoaded = false;
    this.adopting = q.url;
    this.adoptedOverlap = q.overlap;
    this.adoptSrcSet = false;
    this.adoptTimer = this.clock.setTimeout(() => this.abandonAdoption(), ADOPT_TIMEOUT_MS);
    // The provider hears the old track end, exactly as from an element.
    this.dispatchEvent(new Event("ended"));
    old.removeAttribute("src");
    old.load();
  }

  private abandonAdoption(): void {
    this.adoptTimer = null;
    if (!this.adopting) return;
    this.adopting = null;
    this.active.pause();
    this.active.removeAttribute("src");
    this.active.load();
  }

  private finishAdoption(): void {
    if (this.adoptTimer !== null) this.clock.clearTimeout(this.adoptTimer);
    this.adoptTimer = null;
    this.adopting = null;
    this.adoptSrcSet = false;
    this.onAdopt?.(this.adoptedOverlap);
    for (const name of OPENING) this.dispatchEvent(new Event(name));
  }

  // ---- MediaBackend --------------------------------------------------------

  get src(): string {
    return this.adopting ?? this.active.src;
  }
  set src(url: string) {
    if (this.adopting) {
      if (sameURL(url, this.adopting)) {
        this.adoptSrcSet = true;
        return;
      }
      // A change of plan: what plays now is not what the provider wants.
      if (this.adoptTimer !== null) this.clock.clearTimeout(this.adoptTimer);
      this.adoptTimer = null;
      this.adopting = null;
      this.active.pause();
    }
    this.cancelQueued();
    this.active.src = url;
  }

  load(): void {
    if (this.adopting) return;
    this.active.load();
  }

  play(): Promise<void> {
    if (this.adopting) {
      if (this.adoptSrcSet) this.finishAdoption();
      return Promise.resolve();
    }
    return this.active.play();
  }

  pause(): void {
    if (this.adopting) return; // the teardown between two tracks
    if (this.startTimer !== null) this.clock.clearTimeout(this.startTimer);
    this.startTimer = null;
    this.active.pause();
  }

  removeAttribute(name: "src"): void {
    if (this.adopting) return;
    this.cancelQueued();
    this.active.removeAttribute(name);
  }

  get currentTime(): number {
    return this.adopting ? 0 : this.active.currentTime;
  }
  set currentTime(t: number) {
    // A seek moves the end, so any planned start is re-planned on the next
    // timeupdate; one already sounding is left to finish.
    if (this.startTimer !== null) this.clock.clearTimeout(this.startTimer);
    this.startTimer = null;
    this.active.currentTime = t;
  }

  get duration(): number {
    return this.active.duration;
  }
  get paused(): boolean {
    return this.adopting ? false : this.active.paused;
  }
  get volume(): number {
    return this.level;
  }
  set volume(v: number) {
    this.level = v;
    if (this.fadeTimer === null) this.active.volume = v;
  }
  get muted(): boolean {
    return this.active.muted;
  }
  set muted(m: boolean) {
    this.active.muted = m;
    this.standby.muted = m;
  }
  get playbackRate(): number {
    return this.active.playbackRate;
  }
  set playbackRate(r: number) {
    this.active.playbackRate = r;
  }
  get readyState(): number {
    return this.active.readyState;
  }
  get error(): MediaError | null {
    return this.active.error;
  }
  get videoWidth(): number {
    return 0;
  }
  get videoHeight(): number {
    return 0;
  }

  /** The element sounding now, for the audio graph (step 2). */
  elements(): [DeckElement, DeckElement] {
    return [this.active, this.standby];
  }
}

// A URL the provider sets and the one the element reports differ by
// resolution (relative against absolute), so they are compared by path+query.
function sameURL(a: string, b: string): boolean {
  const norm = (u: string) => {
    try {
      const x = new URL(u, "http://deck.invalid");
      return x.pathname + x.search;
    } catch {
      return u;
    }
  };
  return norm(a) === norm(b);
}
