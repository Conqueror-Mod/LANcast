/*
 * Retro games in the desktop client (ADR 0073, stage 2).
 *
 * The page names a game and passes a stream ticket it minted with its own
 * session, exactly as for a film (mpvBackend.ts). The client fetches the
 * game's files, runs the core, draws into the native video window and sends
 * saves to the server; the page draws the menu over the picture and owns
 * nothing else.
 *
 * Everything here degrades to "not in this window": a browser tab has no
 * bindings, and an older desktop client has none of these, which the page
 * reads as "plays in the desktop app" rather than as a broken button.
 */
import { apiPost } from "@/api/client";

/** What the client says about playing one console. */
export interface RetroAvailability {
  available: boolean;
  reason?: string;
  core?: string;
  licence?: string;
  needs_gl?: boolean;
  /** The core is LANcast's to fetch, and is not fetched yet. */
  installable?: boolean;
  /** What fetching it costs: the whole pinned archive, every core at once. */
  download_bytes?: number;
  /** The core is here and cannot boot without a BIOS from the person's console. */
  needs_bios?: boolean;
}

/** Where fetching the cores has got to. */
export interface RetroInstallStatus {
  running: boolean;
  /** "download" in bytes, then "unpack" in cores, then "done". */
  stage: "" | "download" | "unpack" | "done";
  done: number;
  total: number;
  /** Why the last attempt failed; empty when it did not. */
  error: string;
  /** The RetroArch stable release the cores come from. */
  version: string;
  bytes: number;
}

/** One core option, as the running core declared it. */
export interface RetroOption {
  key: string;
  description: string;
  values: string[];
  value: string;
}

/** One event from a running game. */
export interface RetroEvent {
  kind:
    | "loading"
    | "started"
    | "options"
    | "paused"
    | "resumed"
    | "menu"
    | "state-saved"
    | "state-loaded"
    | "sram-saved"
    | "message"
    | "error"
    | "stopped";
  slot?: string;
  text?: string;
  done?: number;
  total?: number;
  options?: RetroOption[];
}

declare global {
  interface Window {
    lancastRetroAvailable?: (platform: string) => Promise<RetroAvailability>;
    lancastRetroOpen?: (
      itemID: number,
      ticket: string,
      platform: string,
      resume: boolean,
    ) => Promise<void>;
    lancastRetroCommand?: (name: string, slot: string) => Promise<void>;
    lancastRetroStop?: () => Promise<void>;
    lancastRetroSetCore?: (platform: string, path: string) => Promise<void>;
    lancastRetroCores?: () => Promise<Record<string, string>>;
    lancastRetroSetOption?: (platform: string, key: string, value: string) => Promise<void>;
    lancastRetroVolume?: () => Promise<number>;
    lancastRetroSetVolume?: (volume: number) => Promise<void>;
    lancastRetroInstallCores?: () => Promise<void>;
    lancastRetroCancelInstallCores?: () => Promise<void>;
    lancastRetroInstallStatus?: () => Promise<RetroInstallStatus>;
    lancastRetroOpenBIOSFolder?: () => Promise<void>;
    __lancastRetroEvent?: (e: RetroEvent) => void;
  }
}

/** Whether this window can run games at all. */
export function retroSupported(): boolean {
  return typeof window !== "undefined" && typeof window.lancastRetroOpen === "function";
}

/** Whether this window can play a console now, and if not, why not. */
export async function retroAvailability(platform: string | null | undefined): Promise<RetroAvailability> {
  if (!retroSupported() || !window.lancastRetroAvailable) {
    return { available: false, reason: "Plays in the LANcast desktop app." };
  }
  if (!platform) {
    return { available: false, reason: "LANcast does not know which console this game is for." };
  }
  try {
    return await window.lancastRetroAvailable(platform);
  } catch {
    return { available: false, reason: "The desktop app could not check its emulator." };
  }
}

const listeners = new Set<(e: RetroEvent) => void>();

/** Listen to the running game. Returns the way to stop listening. */
export function onRetroEvent(fn: (e: RetroEvent) => void): () => void {
  listeners.add(fn);
  if (typeof window !== "undefined") {
    window.__lancastRetroEvent = (e) => {
      for (const l of listeners) l(e);
    };
  }
  return () => {
    listeners.delete(fn);
  };
}

/**
 * Start a game. The ticket is minted here with this page's session and goes
 * straight to the client; it is never put in a URL.
 */
export async function openGame(itemID: number, platform: string, resume: boolean): Promise<void> {
  if (!window.lancastRetroOpen) throw new Error("Games play in the LANcast desktop app.");
  const { ticket } = await apiPost<{ ticket: string }>(`/api/items/${itemID}/stream-ticket`, {});
  await window.lancastRetroOpen(itemID, ticket, platform, resume);
}

export type RetroCommand = "pause" | "resume" | "reset" | "save-state" | "load-state" | "stop";

export async function retroCommand(name: RetroCommand, slot = ""): Promise<void> {
  if (!window.lancastRetroCommand) return;
  await window.lancastRetroCommand(name, slot);
}

export async function stopGame(): Promise<void> {
  if (!window.lancastRetroStop) return;
  await window.lancastRetroStop();
}

/** The numbered save-state slots the menu offers. */
export const STATE_SLOTS = ["state-1", "state-2", "state-3"] as const;

/** "state-2" reads as "Slot 2"; "auto" as where you left off. */
export function slotLabel(slot: string): string {
  if (slot === "auto") return "Where you left off";
  const m = /^state-(\d)$/.exec(slot);
  return m ? `Slot ${m[1]}` : slot;
}

/*
 * The few core options the menu offers (ADR 0073: a short curated list, not
 * every switch a core has). Matched by the end of the key, so the list does
 * not depend on the prefix a particular build of a core gives its options;
 * the core declares them, and only those it declares are shown. Today these
 * are Mupen64Plus-Next's picture options; a console whose core declares none
 * of them simply has no Picture section.
 */
const CURATED_SUFFIXES = ["-43screensize", "-169screensize", "-aspect", "-EnableNativeResFactor"];

export function curatedOptions(options: RetroOption[] | undefined): RetroOption[] {
  if (!options) return [];
  const out: RetroOption[] = [];
  for (const suffix of CURATED_SUFFIXES) {
    const o = options.find((x) => x.key.endsWith(suffix) && x.values.length > 1);
    if (o) out.push(o);
  }
  return out;
}

/** The value after the current one, wrapping round. */
export function nextValue(o: RetroOption): string {
  const i = o.values.indexOf(o.value);
  return o.values[(i + 1) % o.values.length];
}

export async function setRetroOption(platform: string, key: string, value: string): Promise<void> {
  if (!window.lancastRetroSetOption) return;
  await window.lancastRetroSetOption(platform, key, value);
}

/*
 * The game's own volume (ADR 0076). A film can play in the corner while a game
 * runs, and each keeps a volume of its own: the film's is the player's, this
 * one is the game's, and the client applies it to the game's samples alone.
 * The menu offers steps rather than a fader, because the pad is what is in
 * somebody's hands.
 */
export const VOLUME_STEPS = [1, 0.75, 0.5, 0.25, 0] as const;

export function volumeLabel(v: number): string {
  return v <= 0 ? "Off" : `${Math.round(v * 100)}%`;
}

/** The step after the current one, wrapping round; an odd value goes to full. */
export function nextVolume(v: number): number {
  const i = VOLUME_STEPS.findIndex((s) => Math.abs(s - v) < 0.01);
  return VOLUME_STEPS[(i + 1) % VOLUME_STEPS.length];
}

export async function gameVolume(): Promise<number> {
  if (!window.lancastRetroVolume) return 1;
  try {
    return await window.lancastRetroVolume();
  } catch {
    return 1;
  }
}

export async function setGameVolume(v: number): Promise<void> {
  if (!window.lancastRetroSetVolume) return;
  await window.lancastRetroSetVolume(v);
}

/*
 * Whether a game screen is up, for the playback provider (ADR 0076).
 *
 * The docked film goes *above* the page while a game runs ("pip") and above
 * the main window otherwise ("mini"). The provider lives above the router and
 * cannot see which route is showing, and the game screen cannot reach into
 * the provider; this is the one fact they share, so it is a tiny store rather
 * than a context either side would have to be rearranged to provide.
 */
let gameOnScreen = false;
const screenListeners = new Set<() => void>();

export function setGameOnScreen(on: boolean): void {
  if (gameOnScreen === on) return;
  gameOnScreen = on;
  for (const l of screenListeners) l();
}

export function isGameOnScreen(): boolean {
  return gameOnScreen;
}

export function subscribeGameOnScreen(fn: () => void): () => void {
  screenListeners.add(fn);
  return () => {
    screenListeners.delete(fn);
  };
}
