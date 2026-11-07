import { useCallback, useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { apiGet, apiSend } from "@/api/client";
import type { Item, RemoteCommand, RemoteCommandRequest, RemotePlayer } from "@/api/types";
import { isContainer } from "@/lib/kind";

/*
 * Play on another of your own devices
 * ([ADR 0117](../../../docs/adr/0117-play-on-another-device.md)).
 *
 * A window that offers itself is a *player*: it reports what it is playing
 * every few seconds (RemotePlayerHost), and collects the commands your other
 * windows send it when the event stream says `remote`. A window offering
 * itself or not, any window can be the remote.
 *
 * Everything here is per device, like the playback prefs: whether this screen
 * should take films from your phone is a fact about this screen.
 */

export type DeviceKind = "desktop" | "browser" | "phone";

export interface RemoteDevice {
  /** This window's player id. Random, made once, kept. */
  id: string;
  /** What your other devices call this one. */
  name: string;
  /** Whether this window offers itself as a player. */
  enabled: boolean;
}

const KEY = "lancast:remote-device";

/** How often a player reports while nothing changes. */
export const REPORT_MS = 5000;
/** How often an open picker refreshes the list, so a closed laptop drops off. */
export const LIST_MS = 10_000;

/** True inside the LANcast desktop window, which exposes its own bindings. */
export function isDesktopApp(): boolean {
  return typeof (window as { lancastDesktopState?: unknown }).lancastDesktopState === "function";
}

export function deviceKind(ua = navigator.userAgent, desktop = isDesktopApp()): DeviceKind {
  if (desktop) return "desktop";
  if (/Android|iPhone|iPad|iPod|Mobile/i.test(ua)) return "phone";
  return "browser";
}

/*
 * A first name for this device, before anybody gives it one. The browser
 * cannot know the machine's name, so this says what it can and Settings
 * offers to change it.
 */
export function defaultName(ua = navigator.userAgent, desktop = isDesktopApp()): string {
  if (desktop) return "LANcast app";
  if (/iPad/i.test(ua)) return "iPad";
  if (/iPhone|iPod/i.test(ua)) return "iPhone";
  if (/Android/i.test(ua)) return /Mobile/i.test(ua) ? "Android phone" : "Android tablet";
  const os = /Windows/i.test(ua) ? "Windows" : /Mac OS X|Macintosh/i.test(ua) ? "Mac" : /Linux/i.test(ua) ? "Linux" : "";
  const browser = /Edg\//.test(ua) ? "Edge" : /Firefox\//.test(ua) ? "Firefox" : /Chrome\//.test(ua) ? "Chrome" : /Safari\//.test(ua) ? "Safari" : "Browser";
  return os ? `${browser} on ${os}` : browser;
}

/*
 * Whether a device offers itself until somebody says otherwise. The desktop
 * window does. A browser tab or a phone does not: a browser will not start
 * sound without a tap on the page, so a film sent to an idle tab would arrive
 * silent and paused, which looks like the feature not working.
 */
export function enabledByDefault(kind: DeviceKind): boolean {
  return kind === "desktop";
}

function newID(): string {
  const bytes = new Uint8Array(12);
  crypto.getRandomValues(bytes);
  return Array.from(bytes, (b) => (b % 36).toString(36)).join("") + "-" + Date.now().toString(36);
}

function read(): RemoteDevice {
  let stored: Partial<RemoteDevice> = {};
  try {
    stored = JSON.parse(localStorage.getItem(KEY) ?? "{}") as Partial<RemoteDevice>;
  } catch {
    // Unreadable storage is a device seen for the first time.
  }
  const id = typeof stored.id === "string" && /^[A-Za-z0-9_-]{8,64}$/.test(stored.id) ? stored.id : newID();
  const device: RemoteDevice = {
    id,
    name: typeof stored.name === "string" && stored.name.trim() ? stored.name : defaultName(),
    enabled: typeof stored.enabled === "boolean" ? stored.enabled : enabledByDefault(deviceKind()),
  };
  if (device.id !== stored.id) write(device);
  return device;
}

function write(d: RemoteDevice) {
  try {
    localStorage.setItem(KEY, JSON.stringify(d));
  } catch {
    // Without storage the device gets a new id each load, and still works.
  }
}

let current: RemoteDevice | null = null;
const listeners = new Set<(d: RemoteDevice) => void>();

export function getRemoteDevice(): RemoteDevice {
  if (!current) current = read();
  return current;
}

export function setRemoteDevice(patch: Partial<Omit<RemoteDevice, "id">>) {
  current = { ...getRemoteDevice(), ...patch };
  write(current);
  for (const fn of listeners) fn(current);
}

/** Forget what was read, for tests that change storage between cases. */
export function resetRemoteDeviceForTests() {
  current = null;
}

export function useRemoteDevice(): [RemoteDevice, (patch: Partial<Omit<RemoteDevice, "id">>) => void] {
  const [d, setD] = useState(getRemoteDevice);
  useEffect(() => {
    listeners.add(setD);
    setD(getRemoteDevice());
    return () => {
      listeners.delete(setD);
    };
  }, []);
  const update = useCallback((patch: Partial<Omit<RemoteDevice, "id">>) => setRemoteDevice(patch), []);
  return [d, update];
}

/*
 * The things "Play on…" is offered for: whatever plays on the /watch route.
 * A container (a show, an album) has no single thing to send, a book plays in
 * its own player, and a game, a comic, a photo or a channel is not played on
 * that route at all. Leaves of any other kind are sent, since `kind` is an
 * open set (ADR 0018) and an extra is a film like any other.
 */
const NOT_SENT = new Set(["book_part", "rom", "comic", "photo", "channel"]);

export function canSend(item: Item | undefined | null): boolean {
  return !!item && !isContainer(item) && !NOT_SENT.has(item.kind);
}

/** Your other live players: every one but this window. */
export function otherPlayers(players: RemotePlayer[] | undefined, selfID: string): RemotePlayer[] {
  return (players ?? []).filter((p) => p.id !== selfID);
}

/*
 * Your players, while `enabled`. Always refreshed by the `players` topic; an
 * open picker also polls every LIST_MS (`poll`), because a laptop closed
 * without saying so is only noticed by the server's sweep, and the sweep only
 * runs when some player reports.
 */
export function useRemotePlayers(enabled: boolean, poll = true) {
  return useQuery({
    queryKey: ["remote-players"],
    enabled,
    refetchInterval: enabled && poll ? LIST_MS : false,
    queryFn: ({ signal }) => apiGet<{ players: RemotePlayer[] }>("/api/remote/players", signal),
    select: (d) => d.players ?? [],
  });
}

/*
 * Sends one command to one of your players, naming this window as the
 * sender: by its player id when it offers itself, and always by the name it
 * was given, so a phone used only as a remote is still named on the screen
 * it sent to.
 */
export function sendCommand(playerID: string, cmd: RemoteCommandRequest): Promise<void> {
  const self = getRemoteDevice();
  return apiSend(`/api/remote/players/${encodeURIComponent(playerID)}/commands`, "POST", {
    ...cmd,
    from_player: self.enabled ? self.id : undefined,
    from_name: self.name,
  });
}

/*
 * Where a player is now, from its last report. A playing position moves on
 * by how long ago it reported (the server's `age_ms`) plus how long ago this
 * window received the answer, so no two clocks are compared; the same
 * arithmetic a Watch Together follower does.
 */
export function playerPosition(p: RemotePlayer, receivedAtMS: number, nowMS: number): number {
  if (p.paused || !p.item_id) return p.position_ms;
  const pos = p.position_ms + p.age_ms + Math.max(0, nowMS - receivedAtMS);
  return p.duration_ms > 0 ? Math.min(pos, p.duration_ms) : pos;
}

/** What a player needs from the player it drives. A subset of PlaybackState. */
export interface Playable {
  itemID: number;
  playing: boolean;
  stop: () => void;
  togglePlay: () => void;
  seekTo: (t: number) => void;
  playNext: () => void;
  playPrev: () => void;
  changeVolume: (v: number) => void;
}

/*
 * What running one command means.
 *
 * `play` is the odd one out: it opens the player route, as pressing Play
 * would, with the queue and from-start in history state, and leaves a seek
 * pending for when the item has loaded. Every other command acts on what is
 * playing now. Returned as data so the host can do the navigation and the
 * tests can read what a command did.
 */
export type CommandEffect =
  | {
      kind: "open";
      itemID: number;
      queue: number[];
      fromStart: boolean;
      /** Where to seek once loaded; null leaves the usual resume point. */
      seekMS: number | null;
    }
  | { kind: "done" }
  | { kind: "ignored"; reason: string };

export function runCommand(cmd: RemoteCommand, pb: Playable): CommandEffect {
  switch (cmd.action) {
    case "play": {
      const id = cmd.item_id ?? 0;
      if (id <= 0) return { kind: "ignored", reason: "no item" };
      const queue = cmd.queue && cmd.queue.length > 0 ? cmd.queue : [id];
      const seekMS = typeof cmd.position_ms === "number" ? cmd.position_ms : null;
      /*
       * A position is not "from the start", though it skips the resume point
       * too: the server would then offer a pre-roll (ADR 0099), and a film
       * moved here at minute forty would open on a trailer. The host passes
       * the position to the player, which skips both.
       */
      return { kind: "open", itemID: id, queue, fromStart: !!cmd.from_start, seekMS };
    }
    case "pause":
      if (pb.itemID && pb.playing) pb.togglePlay();
      return { kind: "done" };
    case "resume":
      if (pb.itemID && !pb.playing) pb.togglePlay();
      return { kind: "done" };
    case "seek":
      if (!pb.itemID) return { kind: "ignored", reason: "nothing playing" };
      pb.seekTo((cmd.position_ms ?? 0) / 1000);
      return { kind: "done" };
    case "stop":
      if (pb.itemID) pb.stop();
      return { kind: "done" };
    case "next":
      if (pb.itemID) pb.playNext();
      return { kind: "done" };
    case "previous":
      if (pb.itemID) pb.playPrev();
      return { kind: "done" };
    case "volume":
      if (typeof cmd.volume === "number") pb.changeVolume(Math.min(1, Math.max(0, cmd.volume)));
      return { kind: "done" };
    default:
      return { kind: "ignored", reason: `unknown action ${String(cmd.action)}` };
  }
}
