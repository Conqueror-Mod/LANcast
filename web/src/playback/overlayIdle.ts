/*
 * Whether anything is on screen over a native film (ADR 0111).
 *
 * While nothing is, the desktop window takes the page's overlay off the
 * screen, so Windows can send the film straight to the TV instead of
 * composing it with the page on another monitor's clock. That composing was
 * the frame loss behind the hitches during a film.
 *
 * One rule, in one place: anything the page draws over the picture keeps the
 * overlay up, because hiding it hides that thing too.
 */

export interface OverlayState {
  native: boolean;
  chromeVisible: boolean;
  isAudio: boolean;
  playing: boolean;
  loading: boolean;
  /** A subtitle track is showing. The page draws subtitles, not mpv. */
  subtitles: boolean;
  skipCard: boolean;
  upNext: boolean;
  introSkipped: boolean;
  stats: boolean;
  stillWatching: boolean;
}

/**
 * The first thing keeping the overlay up, or "" when nothing is. The reason
 * goes to the client's log, so a film that never went uncomposed says why.
 */
export function overlayKeptBy(s: OverlayState): string {
  if (!s.native) return "not native";
  if (s.chromeVisible) return "controls";
  if (s.isAudio) return "audio";
  if (!s.playing) return "paused";
  if (s.loading) return "loading";
  if (s.subtitles) return "subtitles";
  if (s.skipCard) return "skip card";
  if (s.upNext) return "up next";
  if (s.introSkipped) return "intro skipped";
  if (s.stats) return "stats";
  if (s.stillWatching) return "still watching";
  return "";
}

/** A key the window caught while the overlay was away, as KeyboardEvent names it. */
export interface WakeKey {
  key: string;
  code: string;
  shiftKey: boolean;
}

/**
 * What the window calls when a key or a move over the picture brings the
 * overlay back. The controls show, and a key is then handed to the player's
 * own keydown handler, so Space pauses rather than only waking.
 */
export function nativeWake(wake: () => void, key: WakeKey | null | undefined): void {
  wake();
  if (key) window.dispatchEvent(new KeyboardEvent("keydown", { ...key, bubbles: true }));
}
