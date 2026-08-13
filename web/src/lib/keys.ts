/*
 * The keyboard map, in one place.
 *
 * It lived in Settings, which is where somebody goes to *read about* the keys
 * and not where they are when they want one — that is mid-film, three screens
 * away, with the chrome hidden. The overlay and the settings pane now render
 * the same array, so the two can never drift into disagreeing about what a key
 * does.
 */
export type KeyRow = [keys: string, meaning: string];

export const GLOBAL_KEYS: KeyRow[] = [
  ["Arrows", "Move between tiles and shelves"],
  ["Enter", "Open the focused item"],
  ["Esc", "Back / close"],
  ["/", "Search everything"],
  ["?", "Show this list"],
];

export const PLAYER_KEYS: KeyRow[] = [
  ["Space · K", "Play / pause"],
  ["← · →", "Seek ∓10 seconds"],
  ["↑ · ↓", "Volume up · down"],
  ["F", "Fullscreen"],
  ["M", "Mute"],
  ["[ · ]", "Cycle subtitle track"],
  ["Esc", "Leave fullscreen, then close"],
];

export const ALL_KEYS: KeyRow[] = [...GLOBAL_KEYS, ...PLAYER_KEYS];
