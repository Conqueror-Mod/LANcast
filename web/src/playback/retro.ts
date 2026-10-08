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
  installable?: boolean;
}

/** One event from a running game. */
export interface RetroEvent {
  kind:
    | "loading"
    | "started"
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
