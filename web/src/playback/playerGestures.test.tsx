/*
 * One gesture, one effect.
 *
 * Both bugs here were reported from a real player and neither was a logic
 * error — each was two handlers acting on one gesture.
 *
 * Double-click fired *two* click events and then dblclick, so play toggled
 * twice (a no-op) and the compensating toggle written to cancel "the" click
 * made it an odd number: double-clicking out of fullscreen paused the film.
 * Counting clicks is the wrong tool; waiting is the right one.
 *
 * Escape was wired as a case in the player's own keydown listener, on `window`
 * — while this app resolves Escape centrally on `document`, which fires first.
 * The case could never win, and pressing Escape in fullscreen closed the player
 * and left a borderless window over the library with the film playing on in the
 * corner.
 *
 * These assert the arithmetic of the gestures, which is what nobody can see by
 * looking.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

type Handlers = {
  onClick: () => void;
  onDoubleClick: () => void;
};

/**
 * The click/double-click rule as the player implements it: a click waits, a
 * second click cancels the wait and asks for fullscreen instead.
 */
function gestures(togglePlay: () => void, toggleFullscreen: () => void): Handlers {
  let timer: ReturnType<typeof setTimeout> | undefined;
  return {
    onClick: () => {
      clearTimeout(timer);
      timer = setTimeout(togglePlay, 220);
    },
    onDoubleClick: () => {
      clearTimeout(timer);
      toggleFullscreen();
    },
  };
}

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

describe("clicking the picture", () => {
  it("a single click plays or pauses, once", () => {
    const play = vi.fn();
    const fs = vi.fn();
    const h = gestures(play, fs);

    h.onClick();
    vi.advanceTimersByTime(300);

    expect(play).toHaveBeenCalledTimes(1);
    expect(fs).not.toHaveBeenCalled();
  });

  it("a double click changes fullscreen and nothing else", () => {
    const play = vi.fn();
    const fs = vi.fn();
    const h = gestures(play, fs);

    // What the browser actually sends: click, click, dblclick.
    h.onClick();
    h.onClick();
    h.onDoubleClick();
    vi.advanceTimersByTime(300);

    expect(fs).toHaveBeenCalledTimes(1);
    // The reported bug, in one line: leaving fullscreen must not pause the
    // film.
    expect(play).not.toHaveBeenCalled();
  });

  it("a click after a double click still plays or pauses", () => {
    const play = vi.fn();
    const fs = vi.fn();
    const h = gestures(play, fs);

    h.onClick();
    h.onClick();
    h.onDoubleClick();
    vi.advanceTimersByTime(300);

    h.onClick();
    vi.advanceTimersByTime(300);

    expect(play).toHaveBeenCalledTimes(1);
    expect(fs).toHaveBeenCalledTimes(1);
  });
});

/**
 * Back, as the player registers it on the central stack.
 */
function backHandler(
  isFullscreen: () => boolean,
  exitFullscreen: () => void,
  close: () => void,
) {
  return () => {
    if (isFullscreen()) {
      exitFullscreen();
      return;
    }
    close();
  };
}

describe("Escape", () => {
  it("leaves fullscreen first, and does not close the player", () => {
    const exit = vi.fn();
    const close = vi.fn();
    backHandler(() => true, exit, close)();

    expect(exit).toHaveBeenCalledTimes(1);
    // The reported bug: Escape closed the player and left the window
    // borderless over the library, film still going in the corner.
    expect(close).not.toHaveBeenCalled();
  });

  it("closes the player when there is no fullscreen to leave", () => {
    const exit = vi.fn();
    const close = vi.fn();
    backHandler(() => false, exit, close)();

    expect(close).toHaveBeenCalledTimes(1);
    expect(exit).not.toHaveBeenCalled();
  });
});
