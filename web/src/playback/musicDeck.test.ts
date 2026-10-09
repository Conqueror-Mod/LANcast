/*
 * The music deck, stepped through with fake elements on a fake clock.
 *
 * The question every test answers is the one gapless is about: at the moment
 * the old track stops sounding, is the next one already sounding — and does
 * the provider, which knows nothing of any of this, see exactly the events an
 * element would have given it?
 */
import { describe, it, expect, beforeEach } from "vitest";
import {
  MusicDeck,
  GAPLESS_OVERLAP_S,
  ADOPT_TIMEOUT_MS,
  fadeInGain,
  fadeOutGain,
  type DeckClock,
  type DeckElement,
} from "./musicDeck";

// ---- a clock and elements that only move when the test says so ------------

class FakeClock implements DeckClock {
  now = 0;
  private seq = 1;
  private timers = new Map<number, { at: number; fn: () => void; every?: number }>();
  setTimeout(fn: () => void, ms: number) {
    const id = this.seq++;
    this.timers.set(id, { at: this.now + ms, fn });
    return id;
  }
  clearTimeout(id: number) {
    this.timers.delete(id);
  }
  setInterval(fn: () => void, ms: number) {
    const id = this.seq++;
    this.timers.set(id, { at: this.now + ms, fn, every: ms });
    return id;
  }
  clearInterval(id: number) {
    this.timers.delete(id);
  }
  fireDue() {
    for (const [id, t] of [...this.timers]) {
      if (t.at <= this.now && this.timers.has(id)) {
        if (t.every) t.at += t.every;
        else this.timers.delete(id);
        t.fn();
      }
    }
  }
}

class FakeElement extends EventTarget implements DeckElement {
  src = "";
  currentTime = 0;
  duration = NaN;
  paused = true;
  ended = false;
  volume = 1;
  muted = false;
  playbackRate = 1;
  preload = "";
  readyState = 0;
  error: MediaError | null = null;
  loads = 0;
  constructor(public name: string, private lengths: Record<string, number>) {
    super();
  }
  private fire(type: string) {
    this.dispatchEvent(new Event(type));
  }
  load() {
    this.loads++;
    this.ended = false;
    this.currentTime = 0;
    this.paused = true;
    this.duration = this.src ? this.lengths[this.src] ?? NaN : NaN;
    this.readyState = this.src ? 4 : 0;
    if (this.src) this.fire("loadedmetadata");
  }
  play() {
    if (!this.src) return Promise.resolve();
    this.paused = false;
    this.ended = false;
    this.fire("play");
    this.fire("playing");
    return Promise.resolve();
  }
  pause() {
    if (this.paused) return;
    this.paused = true;
    this.fire("pause");
  }
  removeAttribute() {
    this.src = "";
  }
  /** Advance playback by ms; the end behaves as an element's does. */
  tick(ms: number, timeupdate: boolean) {
    if (this.paused || !this.src) return;
    this.currentTime += ms / 1000;
    if (timeupdate) this.fire("timeupdate");
    if (this.currentTime >= this.duration) {
      this.currentTime = this.duration;
      this.paused = true;
      this.ended = true;
      this.fire("pause");
      this.fire("ended");
    }
  }
}

const A = "/api/items/1/stream";
const B = "/api/items/2/stream";
const C = "/api/items/3/stream";

let clock: FakeClock;
let a: FakeElement;
let b: FakeElement;
let deck: MusicDeck;
let heard: string[];
// When each element was sounding, sampled every step: the gap test reads it.
let silentMs: number;

beforeEach(() => {
  clock = new FakeClock();
  const lengths = { [A]: 30, [B]: 40, [C]: 50 };
  a = new FakeElement("a", lengths);
  b = new FakeElement("b", lengths);
  deck = new MusicDeck(a, b, clock);
  heard = [];
  silentMs = 0;
  for (const name of ["loadedmetadata", "durationchange", "loadeddata", "playing", "timeupdate", "play", "pause", "ended", "error", "waiting"]) {
    deck.addEventListener(name, () => heard.push(name));
  }
});

// Steps of 10 ms: timers, then each element's clock; timeupdate every 250 ms.
function run(ms: number) {
  for (let t = 0; t < ms; t += 10) {
    clock.now += 10;
    clock.fireDue();
    const tu = clock.now % 250 === 0;
    a.tick(10, tu);
    b.tick(10, tu);
    if (a.paused && b.paused && deck.pending) silentMs += 10;
  }
}

// The provider's side of a track change, as PlaybackProvider does it today.
function providerAdvancesTo(url: string) {
  deck.pause();
  deck.removeAttribute("src");
  deck.load();
  deck.src = url;
  deck.load();
  void deck.play();
}

function startA() {
  deck.src = A;
  deck.load();
  void deck.play();
}

describe("a gapless join", () => {
  it("starts the next track before the last one stops, so nothing is ever silent", () => {
    startA();
    run(5000);
    deck.queue({ url: B, overlap: GAPLESS_OVERLAP_S });
    expect(b.src).toBe(B); // loaded on the standby at once
    expect(b.paused).toBe(true);
    run(26_000); // past the end of A at 30 s
    expect(silentMs).toBe(0);
    expect(b.paused).toBe(false);
    // B started a little before A ended, by about the overlap.
    expect(b.currentTime).toBeGreaterThan(1);
  });

  it("tells the provider the old track ended, and nothing of the new one until it is adopted", () => {
    startA();
    run(5000);
    deck.queue({ url: B, overlap: GAPLESS_OVERLAP_S });
    heard = [];
    run(26_000);
    expect(heard).toContain("ended");
    const afterEnd = heard.slice(heard.indexOf("ended") + 1);
    expect(afterEnd).toEqual([]); // B's play/playing/timeupdates are held back
    expect(deck.paused).toBe(false);
    expect(deck.currentTime).toBe(0);

    providerAdvancesTo(B);
    expect(b.paused).toBe(false); // the teardown did not stop it
    expect(heard.slice(-6)).toEqual(["loadedmetadata", "durationchange", "loadeddata", "play", "playing", "timeupdate"]);
    run(1000);
    expect(heard.at(-1)).toBe("timeupdate"); // and now B speaks for itself
    expect(deck.currentTime).toBeGreaterThan(1);
    expect(deck.src).toBe(B);
  });

  it("loads nothing again for the adopted track", () => {
    startA();
    run(5000);
    deck.queue({ url: B, overlap: GAPLESS_OVERLAP_S });
    run(26_000);
    const loads = b.loads;
    providerAdvancesTo(B);
    expect(b.loads).toBe(loads);
  });

  it("joins three tracks in a row the same way", () => {
    startA();
    run(1000);
    deck.queue({ url: B, overlap: GAPLESS_OVERLAP_S });
    run(30_000);
    providerAdvancesTo(B);
    deck.queue({ url: C, overlap: GAPLESS_OVERLAP_S });
    run(40_000);
    providerAdvancesTo(C);
    expect(silentMs).toBe(0);
    expect(deck.src).toBe(C);
    expect(heard.filter((e) => e === "ended")).toHaveLength(2);
  });
});

describe("a change of plan", () => {
  it("aborts the handover when the provider asks for something else", () => {
    startA();
    run(5000);
    deck.queue({ url: B, overlap: GAPLESS_OVERLAP_S });
    run(26_000);
    providerAdvancesTo(C); // the queue moved on to C instead
    expect(deck.src).toBe(C);
    expect(b.src === C || a.src === C).toBe(true);
    expect([a, b].filter((e) => !e.paused && e.src === B)).toHaveLength(0);
  });

  it("stops a handover nobody adopts", () => {
    startA();
    run(5000);
    deck.queue({ url: B, overlap: GAPLESS_OVERLAP_S });
    run(26_000);
    run(ADOPT_TIMEOUT_MS + 100);
    expect(a.paused && b.paused).toBe(true);
  });

  it("cancels a planned start when the queue is cleared or the track is paused", () => {
    startA();
    run(5000);
    deck.queue({ url: B, overlap: GAPLESS_OVERLAP_S });
    deck.queue(null);
    expect(b.src).toBe("");
    run(26_000);
    expect(b.paused).toBe(true);

    startA();
    run(1000);
    deck.queue({ url: B, overlap: GAPLESS_OVERLAP_S });
    run(28_500);
    deck.pause();
    run(2000);
    expect(b.paused).toBe(true);
  });

  it("falls back to the ordinary advance if the next track cannot load", () => {
    startA();
    run(1000);
    deck.queue({ url: B, overlap: GAPLESS_OVERLAP_S });
    b.dispatchEvent(new Event("error"));
    expect(deck.pending).toBe(false);
    run(30_000);
    expect(heard).toContain("ended");
    providerAdvancesTo(B);
    expect(deck.src).toBe(B);
  });
});

describe("a crossfade", () => {
  it("fades the old track out and the new one in over the overlap, at constant power", () => {
    startA();
    run(1000);
    deck.volume = 0.8;
    deck.queue({ url: B, overlap: 5 });
    run(26_500); // 2.5 s into a 5 s fade that began at 25 s
    expect(b.paused).toBe(false);
    const power = a.volume ** 2 + b.volume ** 2;
    expect(power).toBeCloseTo(0.64, 1);
    expect(a.volume).toBeGreaterThan(0.3);
    expect(b.volume).toBeGreaterThan(0.3);
    run(3000);
    providerAdvancesTo(B);
    expect(b.volume).toBeCloseTo(0.8);
  });

  it("has curves that start and end where they should", () => {
    expect(fadeOutGain(0)).toBe(1);
    expect(fadeInGain(0)).toBe(0);
    expect(fadeOutGain(1)).toBeCloseTo(0);
    expect(fadeInGain(1)).toBe(1);
  });
});

describe("without a plan, the deck is the element", () => {
  it("passes play, pause, seek and the end straight through", () => {
    startA();
    run(1000);
    deck.currentTime = 10;
    expect(a.currentTime).toBe(10);
    deck.pause();
    expect(heard).toContain("pause");
    void deck.play();
    run(25_000);
    expect(heard).toContain("ended");
    expect(b.src).toBe("");
  });
});
