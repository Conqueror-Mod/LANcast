/*
 * The mpv backend: native playback in the desktop client (ADR 0067).
 *
 * It implements the same MediaBackend contract as the <video> element
 * (backend.ts) and raises the same events, so PlaybackProvider drives it with
 * the code it already has. The difference is where the work happens: the
 * desktop client's libmpv plays the file's own bytes — Matroska, AC-3, DTS,
 * TrueHD, HEVC — with no conversion on the server.
 *
 * What the page hands over is an item id and a stream ticket it minted with its
 * own session (ADR 0068). It never hands the client a URL: the client's relay
 * builds the only one mpv will open.
 */
import { apiPost } from "@/api/client";
import type { MediaBackend } from "./backend";

interface MpvEvent {
  events: string[];
  current_time: number;
  duration: number | null;
  paused: boolean;
  ended: boolean;
}

declare global {
  interface Window {
    lancastMpvAvailable?: () => Promise<boolean>;
    lancastMpvOpen?: (itemID: number, ticket: string) => Promise<void>;
    lancastMpvCommand?: (name: string, value: number) => Promise<void>;
    lancastMpvStop?: () => Promise<void>;
    lancastMpvLayout?: (
      layout: "full" | "mini" | "hidden",
      x: number,
      y: number,
      width: number,
      height: number,
    ) => Promise<void>;
    __lancastMpvEvent?: (e: MpvEvent) => void;
  }
}

let availability: Promise<boolean> | null = null;

/** Whether this window can play natively. Asked once per page load. */
export function nativePlaybackAvailable(): Promise<boolean> {
  if (!availability) {
    availability = window.lancastMpvAvailable
      ? window.lancastMpvAvailable().catch(() => false)
      : Promise.resolve(false);
  }
  return availability;
}

/** Test seam: forget the cached answer. */
export function resetNativePlaybackAvailability(): void {
  availability = null;
}

/** The item a direct-play source names, or null for anything else. */
export function streamItem(src: string): number | null {
  const m = /^\/api\/stream\/(\d+)(?:\?|$)/.exec(src);
  return m ? Number(m[1]) : null;
}

/** The class on <html> while native video is on screen, which makes the page
 *  see-through where the picture is (playback.css). */
export const NATIVE_VIDEO_CLASS = "lancast-native-video";

class NativeMediaError {
  readonly MEDIA_ERR_ABORTED = 1;
  readonly MEDIA_ERR_NETWORK = 2;
  readonly MEDIA_ERR_DECODE = 3;
  readonly MEDIA_ERR_SRC_NOT_SUPPORTED = 4;
  constructor(
    readonly code: number,
    readonly message: string,
  ) {}
}

export class MpvBackend extends EventTarget implements MediaBackend {
  src = "";
  /** mpv's `aid` to select once the file opens (nativeTracks.ts), or null. */
  audioTrack: number | null = null;
  readonly videoWidth = 0;
  readonly videoHeight = 0;

  private time = 0;
  private dur = NaN;
  private isPaused = true;
  private loaded = false;
  private err: MediaError | null = null;
  private vol = 1;
  private isMuted = false;
  private rate = 1;
  // A seek asked for before the file is open, applied when it is.
  private pendingSeek: number | null = null;
  // Which load the current events belong to; a stale open must not win.
  private generation = 0;

  constructor() {
    super();
    window.__lancastMpvEvent = (e) => this.receive(e);
  }

  get currentTime(): number {
    return this.time;
  }
  set currentTime(t: number) {
    this.time = t;
    if (!this.loaded) {
      this.pendingSeek = t;
      return;
    }
    void this.command("seek", t);
  }

  get duration(): number {
    return this.dur;
  }
  get paused(): boolean {
    return this.isPaused;
  }
  get readyState(): number {
    return this.loaded ? 4 : 0;
  }
  get error(): MediaError | null {
    return this.err;
  }

  get volume(): number {
    return this.vol;
  }
  set volume(v: number) {
    this.vol = Math.min(1, Math.max(0, v));
    void this.command("volume", this.vol);
  }
  get muted(): boolean {
    return this.isMuted;
  }
  set muted(m: boolean) {
    this.isMuted = m;
    void this.command("mute", m ? 1 : 0);
  }
  get playbackRate(): number {
    return this.rate;
  }
  set playbackRate(r: number) {
    this.rate = r;
    void this.command("speed", r);
  }

  load(): void {
    // The element treats load() with no source as a reset, and the provider
    // calls it that way after removing the source.
    if (!this.src) return;
    const gen = ++this.generation;
    this.loaded = false;
    this.err = null;
    this.time = 0;
    this.dur = NaN;
    const id = streamItem(this.src);
    if (id === null) {
      // The provider only hands this backend direct-play sources. Anything
      // else is a bug upstream, and saying "unsupported" routes it into the
      // provider's existing failure handling rather than a silent black
      // screen.
      this.fail(4, `native playback cannot open ${this.src}`);
      return;
    }
    void (async () => {
      try {
        const t = await apiPost<{ ticket: string }>(
          `/api/items/${id}/stream-ticket`,
          {},
        );
        if (gen !== this.generation) return;
        document.documentElement.classList.add(NATIVE_VIDEO_CLASS);
        await window.lancastMpvOpen!(id, t.ticket);
        // play() usually arrives before the player exists, and the client opens
        // files paused; honour it now that there is something to play.
        if (gen === this.generation && this.audioTrack !== null) {
          await this.command("audio", this.audioTrack);
        }
        if (gen === this.generation && !this.isPaused) await this.command("play", 0);
      } catch (e) {
        if (gen !== this.generation) return;
        this.fail(2, e instanceof Error ? e.message : String(e));
      }
    })();
  }

  play(): Promise<void> {
    this.isPaused = false;
    return this.command("play", 0);
  }

  pause(): void {
    this.isPaused = true;
    void this.command("pause", 0);
  }

  removeAttribute(name: "src"): void {
    if (name !== "src") return;
    this.generation++;
    this.src = "";
    this.loaded = false;
    document.documentElement.classList.remove(NATIVE_VIDEO_CLASS);
    void window.lancastMpvStop?.();
  }

  private command(name: string, value: number): Promise<void> {
    return (window.lancastMpvCommand?.(name, value) ?? Promise.resolve()).catch(
      () => {},
    );
  }

  private fail(code: number, message: string): void {
    this.err = new NativeMediaError(code, message) as unknown as MediaError;
    this.dispatchEvent(new Event("error"));
  }

  /** One report from the client, already translated into element events. */
  receive(e: MpvEvent): void {
    this.time = e.current_time;
    this.dur = e.duration ?? NaN;
    this.isPaused = e.paused;
    for (const name of e.events) {
      /*
       * A file that has just opened reports a position of zero until the
       * resume seek lands, and that zero is not where anything is.
       *
       * The provider records every timeupdate as the live position, and reads
       * the live position when it rebuilds a source — so one tick of "we are
       * at 0:00" between load and seek made the *next* rebuild fall back to
       * the saved progress instead, which is up to five seconds stale. Found
       * by changing the audio track twice: the first change held its place,
       * the second went back nine seconds.
       */
      if (name === "timeupdate" && this.pendingSeek !== null) continue;
      if (name === "loadedmetadata") {
        this.loaded = true;
        // Re-assert what the element would have kept across a load.
        void this.command("volume", this.vol);
        void this.command("mute", this.isMuted ? 1 : 0);
        void this.command("speed", this.rate);
      }
      this.dispatchEvent(new Event(name));
      if (name === "loadedmetadata" && this.pendingSeek !== null) {
        const at = this.pendingSeek;
        this.pendingSeek = null;
        if (at > 0) void this.command("seek", at);
      }
    }
  }
}

let shared: MpvBackend | null = null;

/** The one native backend for this page; there is one mpv per window. */
export function mpvBackend(): MpvBackend {
  if (!shared) shared = new MpvBackend();
  return shared;
}
