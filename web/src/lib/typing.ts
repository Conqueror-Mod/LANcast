/*
 * Whether keys pressed now are text somebody is typing, which the player must
 * leave alone.
 *
 * A text box, a text area, or anything editable. **Not a slider.** Sliders are
 * <input> elements too, and the player used to give way to every <input>: once
 * the volume slider or the seek bar had been touched, the arrow keys moved that
 * slider instead of seeking, until a click somewhere else took focus away.
 * Reported as "seeking needs very direct focus, sometimes a click"; a
 * diagnostic build showed the arrows arriving with the volume slider focused
 * (2026-10-17). Buttons, checkboxes and sliders do not take typing, so the
 * transport keys stay the player's.
 */
const NOT_TEXT = new Set([
  "range",
  "button",
  "submit",
  "reset",
  "checkbox",
  "radio",
  "color",
  "file",
  "image",
]);

export function typingInto(el: Element | null): boolean {
  if (!(el instanceof HTMLElement)) return false;
  if (el.isContentEditable || el.tagName === "TEXTAREA") return true;
  if (el instanceof HTMLInputElement) return !NOT_TEXT.has(el.type);
  return false;
}
