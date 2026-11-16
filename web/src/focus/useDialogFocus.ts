import { useCallback, useLayoutEffect, useRef } from "react";
import { useFocusScope, useFocusZone } from "./FocusController";

/*
 * A dialog a remote can use: put the returned ref on the dialog box.
 *
 * Written because the dialogs opened from a tile or the detail page could not
 * be reached without a Tab key (R16-1). Their buttons were plain <button>s that
 * nothing registered with the focus controller, and nothing kept the arrows
 * inside them, so on a television the arrows walked the grid *behind* the
 * dialog and OK opened whichever tile was focused there. PictureEditor already
 * did this by hand; this is that, once, so the next dialog cannot forget it.
 *
 * - **Registered.** Every control inside is a focus-zone entry, so arrows and
 *   OK reach it — including ones that appear later (search results, a list
 *   that was loading).
 * - **Scoped.** Arrows never leave it while it is open.
 * - **Focused on open.** The first control, unless something inside already
 *   took focus (an autoFocus text box) or one is marked `data-autofocus`. The
 *   ✕ is passed over when there is anything else: a remote's first OK should
 *   do something, not close what it just opened.
 * - **Tab stays in.** Tab and Shift+Tab wrap at the ends.
 * - **Focus goes back** to whatever opened it, if that is still on the page
 *   and focus would otherwise be lost.
 */

const FOCUSABLE =
  "button, a[href], input:not([type=hidden]), select, textarea, [tabindex]:not([tabindex='-1'])";

function controls(root: HTMLElement): HTMLElement[] {
  return [...root.querySelectorAll<HTMLElement>(FOCUSABLE)].filter(
    (el) => !(el as HTMLButtonElement).disabled && !el.hidden,
  );
}

function firstControl(root: HTMLElement): HTMLElement | undefined {
  const marked = root.querySelector<HTMLElement>("[data-autofocus]");
  if (marked) return marked;
  const all = controls(root);
  return all.find((el) => el.getAttribute("aria-label") !== "Close") ?? all[0];
}

export function useDialogFocus<T extends HTMLElement = HTMLDivElement>(): (el: T | null) => void {
  const zone = useFocusZone();
  const scope = useFocusScope();
  const node = useRef<T | null>(null);
  const ref = useCallback(
    (el: T | null) => {
      node.current = el;
      zone(el);
      scope(el);
    },
    [zone, scope],
  );

  // Once per dialog, not per render: a layout effect so focus has moved before
  // the browser paints the dialog, and its cleanup runs as the dialog goes.
  useLayoutEffect(() => {
    const root = node.current;
    if (!root) return;
    const active = document.activeElement as HTMLElement | null;
    const opener = active && active !== document.body && !root.contains(active) ? active : null;
    if (!root.contains(document.activeElement)) firstControl(root)?.focus();

    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Tab") return;
      const all = controls(root);
      if (all.length === 0) return;
      const first = all[0];
      const last = all[all.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    };
    root.addEventListener("keydown", onKey);
    return () => {
      root.removeEventListener("keydown", onKey);
      const now = document.activeElement;
      const lost = !now || now === document.body || !now.isConnected || root.contains(now);
      if (lost && opener?.isConnected) opener.focus();
    };
  }, []);

  return ref;
}
