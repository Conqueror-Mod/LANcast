import { readDevice, useDevice, writeDevice } from "./device";

/*
 * The keyboard map, in one place — and now a map that can be changed.
 *
 * It began as an array of two strings per row, rendered by both the overlay and
 * the settings pane so the two could never disagree about what a key does. That
 * property is the one worth keeping, so the rows became *bindings* rather than
 * being replaced by them: an id, a default, a scope, and the same human
 * sentence. Everything that rendered the old shape renders the new one.
 *
 * Why this is customizable at all: the defaults assume a keyboard laid out like
 * the one they were written on. `[` and `]` for subtitle tracks are one key on
 * a US layout and a dead key on several European ones, and a shortcut you
 * cannot physically press is not a shortcut. The map is also the input surface
 * for a remote, where the useful bindings are whatever that remote emits.
 *
 * Stored per device, because the keyboard is a property of the device.
 */

export const KEYS_STORAGE_KEY = "lancast:keybindings";

export type KeyScope = "global" | "player";

export interface Binding {
  id: string;
  /** What it does, in the words the overlay shows. */
  meaning: string;
  scope: KeyScope;
  /**
   * The keys, as `KeyboardEvent.key` values. More than one where a binding has
   * always had alternatives (Space and K both pause) or where the action is
   * inherently a pair (left and right seek in opposite directions) — those pairs
   * stay together, because rebinding half of "seek" is not a thing anybody
   * means to do.
   */
  keys: string[];
  /**
   * Fixed bindings cannot be rebound. Escape and the arrows are how you leave a
   * screen and how you move between tiles; a map that can strand you on a page
   * you cannot escape is a trap, not a customizer.
   */
  fixed?: boolean;
}

export const DEFAULT_BINDINGS: Binding[] = [
  { id: "move", meaning: "Move between tiles and shelves", scope: "global", keys: ["Arrows"], fixed: true },
  { id: "open", meaning: "Open the focused item", scope: "global", keys: ["Enter"], fixed: true },
  { id: "back", meaning: "Back / close", scope: "global", keys: ["Escape"], fixed: true },
  { id: "search", meaning: "Search everything", scope: "global", keys: ["/"] },
  { id: "help", meaning: "Show this list", scope: "global", keys: ["?"] },
  { id: "bigscreen", meaning: "Bigscreen mode", scope: "global", keys: ["Ctrl+Shift+B"], fixed: true },

  { id: "playpause", meaning: "Play / pause", scope: "player", keys: [" ", "k"] },
  { id: "seek", meaning: "Seek ∓10 seconds", scope: "player", keys: ["ArrowLeft", "ArrowRight"], fixed: true },
  { id: "volume", meaning: "Volume up · down", scope: "player", keys: ["ArrowUp", "ArrowDown"], fixed: true },
  { id: "fullscreen", meaning: "Fullscreen", scope: "player", keys: ["f"] },
  { id: "mute", meaning: "Mute", scope: "player", keys: ["m"] },
  { id: "subtitles", meaning: "Cycle subtitle track", scope: "player", keys: ["[", "]"] },
];

/** Overrides are stored as id → keys, not as whole bindings. */
export type KeyOverrides = Record<string, string[]>;

/*
 * Merging, and why the defaults are not stored.
 *
 * Persisting the whole map would freeze it: a binding added in a later version
 * would be invisible to everybody who had ever opened this pane, because their
 * stored copy has no row for it and their stored copy is what renders. Storing
 * only what somebody deliberately changed means new bindings arrive on upgrade,
 * and a removed one disappears rather than lingering as a row that does nothing.
 */
export function mergeBindings(overrides: KeyOverrides): Binding[] {
  return DEFAULT_BINDINGS.map((b) => {
    const custom = overrides[b.id];
    if (b.fixed || !custom || custom.length === 0) return b;
    return { ...b, keys: custom };
  });
}

export function currentBindings(): Binding[] {
  return mergeBindings(readDevice<KeyOverrides>(KEYS_STORAGE_KEY, {}));
}

/** The key a binding answers to, for the components that listen for one. */
export function bindingKeys(id: string): string[] {
  return currentBindings().find((b) => b.id === id)?.keys ?? [];
}

/** True when this event is the given binding. */
export function matchesBinding(id: string, key: string): boolean {
  return bindingKeys(id).includes(key);
}

export function useBindings(): {
  bindings: Binding[];
  rebind: (id: string, keys: string[]) => void;
  reset: (id: string) => void;
  resetAll: () => void;
  changed: (id: string) => boolean;
} {
  const [overrides, setOverrides] = useDevice<KeyOverrides>(KEYS_STORAGE_KEY, {});
  return {
    bindings: mergeBindings(overrides),
    rebind: (id, keys) => setOverrides({ ...overrides, [id]: keys }),
    reset: (id) => {
      const next = { ...overrides };
      delete next[id];
      setOverrides(next);
    },
    resetAll: () => writeDevice<KeyOverrides>(KEYS_STORAGE_KEY, {}),
    changed: (id) => id in overrides,
  };
}

/*
 * A key as somebody would say it out loud.
 *
 * `" "` is the space bar and reads as nothing at all; `ArrowLeft` is an
 * identifier, not a word. This is presentation only — the stored value stays
 * the raw `KeyboardEvent.key`, because a display name is a thing that changes.
 */
const KEY_LABELS: Record<string, string> = {
  " ": "Space",
  ArrowLeft: "←",
  ArrowRight: "→",
  ArrowUp: "↑",
  ArrowDown: "↓",
  Escape: "Esc",
  Enter: "Enter",
};

export function keyLabel(key: string): string {
  if (KEY_LABELS[key]) return KEY_LABELS[key];
  return key.length === 1 ? key.toUpperCase() : key;
}

export function bindingLabel(b: Binding): string {
  return b.keys.map(keyLabel).join(" · ");
}

/*
 * The old shape, still exported.
 *
 * KeyHelp and the settings pane render these; keeping the name means the
 * bindings change did not become a change to every component that shows them.
 */
export type KeyRow = [keys: string, meaning: string];

function rows(scope: KeyScope): KeyRow[] {
  return currentBindings()
    .filter((b) => b.scope === scope)
    .map((b) => [bindingLabel(b), b.meaning] as KeyRow);
}

export function globalKeys(): KeyRow[] {
  return rows("global");
}
export function playerKeys(): KeyRow[] {
  return rows("player");
}
export function allKeys(): KeyRow[] {
  return [...globalKeys(), ...playerKeys()];
}
