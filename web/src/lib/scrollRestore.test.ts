/*
 * When to stop trying to restore a scroll position.
 *
 * The rule that was wrong: give up after twelve animation frames. Fine for a
 * detail page, useless for a browse grid — returning to a library scrolled into
 * the Z's, the document is one page of posters tall for far longer than 200ms,
 * so scrollTo is clamped, the loop gives up, and the grid sits at the top. It
 * reads as the paging having reset, because landing back in the A's is what a
 * reset looks like.
 */
import { describe, it, expect } from "vitest";
import { shouldKeepTrying, RESTORE_BUDGET_MS } from "./useScrollRestoration";

describe("restoring a scroll position", () => {
  // The case the old rule failed: the position is not reachable *yet*, because
  // the content that makes the document tall has not rendered.
  it("keeps trying while the document is still too short", () => {
    expect(
      shouldKeepTrying({ reached: false, cancelled: false, elapsedMs: 250 }),
    ).toBe(true);
    expect(
      shouldKeepTrying({ reached: false, cancelled: false, elapsedMs: 1500 }),
    ).toBe(true);
  });

  it("stops the moment the position is reached", () => {
    expect(
      shouldKeepTrying({ reached: true, cancelled: false, elapsedMs: 16 }),
    ).toBe(false);
  });

  /*
   * A scroll of the user's own wins immediately. A three-second budget would
   * otherwise be three seconds of the page dragging somebody back, which is a
   * worse bug than the one being fixed.
   */
  it("stops immediately when the user scrolls", () => {
    expect(
      shouldKeepTrying({ reached: false, cancelled: true, elapsedMs: 16 }),
    ).toBe(false);
  });

  // A page genuinely shorter than the saved offset never reaches it, so the
  // budget is what ends it rather than a spin.
  it("gives up when the budget runs out", () => {
    expect(
      shouldKeepTrying({
        reached: false,
        cancelled: false,
        elapsedMs: RESTORE_BUDGET_MS,
      }),
    ).toBe(false);
  });

  // Long enough for a large grid to render, which is the whole point of the
  // change; a value back near the old 200ms would silently restore the bug.
  it("allows enough time for a large grid to render", () => {
    expect(RESTORE_BUDGET_MS).toBeGreaterThanOrEqual(2000);
  });
});
