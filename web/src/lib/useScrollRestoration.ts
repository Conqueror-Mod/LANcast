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

/*
 * How long to keep trying, in milliseconds.
 *
 * This was twelve animation frames — about 200ms — and that is the bug it now
 * exists to fix. Twelve frames is plenty for a detail page and nowhere near
 * enough for a browse grid: returning to a library scrolled into the Z's, the
 * document is one page of posters tall for far longer than 200ms, `scrollTo`
 * is clamped against a short document, the loop gives up, and the grid is left
 * at the top. It looked like the paging had reset, because landing back in the
 * A's is what a reset looks like — and it got worse the larger the library,
 * which is the opposite of what a timing-independent cache bug would do.
 *
 * Three seconds is long enough for a large grid to render and its images to
 * lay out, and costs nothing when the position is reached on the first frame,
 * which is the common case. A scroll of the user's own ends it immediately, so
 * a budget this long cannot turn into the page fighting somebody.
 */
export const RESTORE_BUDGET_MS = 3000;

/*
 * Whether to keep trying to restore.
 *
 * Pure, because the rule is the part worth testing and the requestAnimationFrame
 * loop is the part worth reading. It deliberately does not stop when the
 * document has "stopped growing": a grid that pauses between a page arriving
 * and its images laying out has stopped growing several times before it is
 * finished, and every one of those pauses used to be a reason to give up.
 */
export function shouldKeepTrying(o: {
  reached: boolean;
  cancelled: boolean;
  elapsedMs: number;
}): boolean {
  if (o.cancelled || o.reached) return false;
  return o.elapsedMs < RESTORE_BUDGET_MS;
}

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

    const startedAt = Date.now();
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
      /*
       * Keep going until the document agrees, or the budget runs out.
       *
       * A page genuinely shorter than the saved offset never reaches it and
       * simply costs the budget — harmless, because scrollTo clamps and the
       * user's own scroll cancels. The alternative, giving up as soon as the
       * position is unreachable, is what left a large grid at the top: at the
       * moment it is asked, the position is always unreachable.
       */
      if (
        !shouldKeepTrying({
          reached: Math.abs(window.scrollY - target) < 1,
          cancelled,
          elapsedMs: Date.now() - startedAt,
        })
      ) {
        window.removeEventListener("wheel", stopOnUserScroll);
        window.removeEventListener("touchstart", stopOnUserScroll);
        window.removeEventListener("keydown", stopOnUserScroll);
        return;
      }
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
