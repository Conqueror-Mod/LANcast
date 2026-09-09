import type { Prefs, SubFont } from "./prefs";

/*
 * Subtitle appearance, as custom properties on a document root.
 *
 * # Why properties and not a style attribute
 *
 * `::cue` is the only hook there is. The cue box lives in a shadow tree with no
 * element the page can reach, so nothing can be styled inline and the rule in
 * playback.css has to read values from somewhere. Custom properties on the root
 * are that somewhere.
 *
 * # Why this is a module and not three lines in the provider
 *
 * Because it was three lines in the provider, and they wrote to
 * `document.documentElement` — the *page's* root. The pop-out player (ADR 0029)
 * is a second document with a root of its own. `copyStyles` carries the
 * stylesheets over, and an inline style on an element is not a stylesheet, so
 * every `var(--cue-…, fallback)` in the copied rules resolved to its fallback:
 * subtitles at the shipped defaults in the pop-out window, whatever anyone had
 * chosen, with nothing failing anywhere.
 *
 * A function taking the root it writes to makes the second document impossible
 * to forget, and CUE_VARS makes a property that is declared but never applied a
 * failing test rather than a preference that quietly does nothing.
 */

/**
 * Every property the `::cue` rules read.
 *
 * Shared with the tests so that adding a control without applying it fails
 * rather than shipping as a setting with no effect.
 */
export const CUE_VARS = [
  "--cue-color",
  "--cue-scale",
  "--cue-bottom",
  "--cue-font",
] as const;

/*
 * The typefaces, as stacks rather than names.
 *
 * A name alone is a bet that the machine has that face; when it does not, the
 * engine falls back to its own default, which for a cue container is a serif no
 * one asked for. Each of these ends in a generic family, so the worst case is
 * the right *kind* of face rather than an arbitrary one.
 *
 * No font is bundled. A high-legibility face designed for dyslexia — OpenDyslexic
 * and its relatives — is the obvious thing to want here and is deliberately not
 * offered, because shipping one is a licence decision and a bundle-size decision
 * that a dropdown does not get to make on its own. Offering the name without the
 * file would be a control that works on the two machines that happen to have it
 * installed, which is worse than not offering it.
 */
export const FONT_STACKS: Record<SubFont, string> = {
  sans: 'Inter, "Segoe UI", system-ui, sans-serif',
  serif: 'Georgia, "Times New Roman", serif',
  /*
   * Every character the same width, which is the point rather than a style
   * preference: it is the one stack here that cannot reflow a two-line cue into
   * three between one frame and the next.
   */
  mono: 'ui-monospace, "Cascadia Mono", "Consolas", monospace',
};

/** The labels the settings panel offers, in the order it offers them. */
export const FONTS: { value: SubFont; label: string }[] = [
  { value: "sans", label: "Sans-serif" },
  { value: "serif", label: "Serif" },
  { value: "mono", label: "Monospace" },
];

/**
 * applyCueVars writes the appearance preferences onto one document root.
 *
 * Call it for every root that renders a cue — the page and the pop-out window
 * are both places subtitles are read.
 */
export function applyCueVars(root: HTMLElement, prefs: Prefs): void {
  const s = root.style;
  s.setProperty("--cue-color", prefs.subColor);
  s.setProperty("--cue-scale", String(prefs.subSize));
  // Position moves the cue *box*, which is why it is a `bottom` on the
  // container rather than anything `::cue` could express.
  s.setProperty("--cue-bottom", `${prefs.subPosition}%`);
  s.setProperty("--cue-font", FONT_STACKS[prefs.subFont] ?? FONT_STACKS.sans);
}
