import { useEffect, useRef } from "react";
import { useLocation, useNavigationType } from "react-router-dom";

/*
 * Scroll position, managed by us rather than by the browser.
 *
 * The symptom this fixes: press Back from a playing item and the item's page
 * arrives scrolled to the bottom, with the Back button off the top of the
 * screen.
 *
 * The cause is that a single-page app and the browser's automatic restoration
 * disagree about when a page exists. On a history pop Chrome restores the
 * position it recorded for that entry — but it does so against whatever is in
 * the document at that instant, which is the player being torn down, not the
 * detail page that has yet to render or fetch. The number is applied, clamped
 * against the wrong height, and then the real content arrives underneath it.
 * Where it lands is not meaningful, and "the bottom" is one of the places it
 * lands.
 *
 * So: turn the automatic behaviour off and do it deliberately. A new
 * navigation starts at the top, which is what pressing a link means. A back or
 * forward returns to where that entry actually was, which is what pressing Back
 * means — you get the track list where you left it rather than the top of a
 * page you had already scrolled past.
 *
 * Restoring is retried across a few frames because the height it needs may not
 * exist yet: a detail page is a fetch, and scrollTo against a short document is
 * silently clamped. Retrying until the position sticks (or the attempts run
 * out) is the difference between this working on a warm cache and working on a
 * cold one.
 */

// Long enough to cover a fetch that resolves quickly and a couple of layout
// passes; short enough that it cannot fight a user who scrolls immediately.
// Any scroll of their own cancels it — see the listener below.
const RESTORE_FRAMES = 12;

export function useScrollRestoration() {
  const location = useLocation();
  const navigationType = useNavigationType();
  const positions = useRef(new Map<string, number>());

  useEffect(() => {
    if (!("scrollRestoration" in window.history)) return;
    const previous = window.history.scrollRestoration;
    window.history.scrollRestoration = "manual";
    return () => {
      window.history.scrollRestoration = previous;
    };
  }, []);

  useEffect(() => {
    const key = location.key;
    const saved = positions.current.get(key) ?? 0;
    const target = navigationType === "POP" ? saved : 0;

    let frame = 0;
    let cancelled = false;

    // A scroll the user performs themselves ends the restore attempt. Without
    // this, someone who scrolls during a slow load gets yanked back for the
    // next few frames, which feels like the page fighting them.
    const stopOnUserScroll = () => {
      cancelled = true;
    };

    const step = () => {
      if (cancelled) return;
      window.scrollTo(0, target);
      // Done when the document agrees, or when it cannot: a page genuinely
      // shorter than the saved offset will never reach it, and retrying
      // forever would be a spin.
      if (Math.abs(window.scrollY - target) < 1 || frame >= RESTORE_FRAMES) {
        window.removeEventListener("wheel", stopOnUserScroll);
        window.removeEventListener("touchstart", stopOnUserScroll);
        window.removeEventListener("keydown", stopOnUserScroll);
        return;
      }
      frame += 1;
      requestAnimationFrame(step);
    };

    window.addEventListener("wheel", stopOnUserScroll, { passive: true });
    window.addEventListener("touchstart", stopOnUserScroll, { passive: true });
    window.addEventListener("keydown", stopOnUserScroll);
    requestAnimationFrame(step);

    // Record where this entry was left. Reading on the way out rather than on
    // every scroll event keeps this off the scroll path entirely.
    const save = () => positions.current.set(key, window.scrollY);
    window.addEventListener("pagehide", save);

    return () => {
      cancelled = true;
      save();
      window.removeEventListener("pagehide", save);
      window.removeEventListener("wheel", stopOnUserScroll);
      window.removeEventListener("touchstart", stopOnUserScroll);
      window.removeEventListener("keydown", stopOnUserScroll);
    };
  }, [location.key, navigationType]);
}
