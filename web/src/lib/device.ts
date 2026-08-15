import { useSyncExternalStore } from "react";

/*
 * Settings that belong to this device, not to the server.
 *
 * The settings screen already draws the distinction that matters — a server
 * setting is shared and admin-only, a device setting is yours and affects
 * nobody else — and until now the device half had no storage of its own.
 * `lancast:volume` and the playback preferences each grew their own
 * localStorage handling, which is fine for two and a mess at five.
 *
 * localStorage rather than the server, deliberately. "Make the text large
 * because I am ten feet away" is a fact about the room this device is in, and
 * syncing it would change the phone in somebody's hand because the television
 * downstairs is a television.
 *
 * useSyncExternalStore rather than a context: these are read in a handful of
 * places and written in one, and a provider wrapping the app so that two
 * components can agree about a boolean is a provider that will be there
 * forever.
 */

type Listener = () => void;

const listeners = new Map<string, Set<Listener>>();
// The parsed value, kept so getSnapshot returns a stable reference for objects.
// Returning a fresh parse each time makes useSyncExternalStore re-render on
// every store read, which for a keybinding map is every keystroke.
const cache = new Map<string, unknown>();

function subscribe(key: string, fn: Listener): () => void {
  let set = listeners.get(key);
  if (!set) listeners.set(key, (set = new Set()));
  set.add(fn);
  return () => set!.delete(fn);
}

function notify(key: string) {
  listeners.get(key)?.forEach((fn) => fn());
}

export function readDevice<T>(key: string, fallback: T): T {
  if (cache.has(key)) return cache.get(key) as T;
  let value = fallback;
  try {
    const raw = localStorage.getItem(key);
    if (raw !== null) value = JSON.parse(raw) as T;
  } catch {
    // A corrupt or unavailable store is the default, not an error to surface.
    // Private browsing throws on access, and a settings screen that cannot
    // render because a preference could not be read is worse than one showing
    // defaults.
  }
  cache.set(key, value);
  return value;
}

export function writeDevice<T>(key: string, value: T): void {
  cache.set(key, value);
  try {
    localStorage.setItem(key, JSON.stringify(value));
  } catch {
    // Written to memory regardless, so the setting holds for this session even
    // where it cannot be persisted.
  }
  notify(key);
}

/** A device setting as React state. Writes persist and update every reader. */
export function useDevice<T>(key: string, fallback: T): [T, (v: T) => void] {
  const value = useSyncExternalStore(
    (fn) => subscribe(key, fn),
    () => readDevice(key, fallback),
    () => fallback,
  );
  return [value, (v: T) => writeDevice(key, v)];
}
