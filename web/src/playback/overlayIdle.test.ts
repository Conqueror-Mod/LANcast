import { describe, expect, it, vi } from "vitest";
import { nativeWake, overlayKeptBy, type OverlayState } from "./overlayIdle";

const idle: OverlayState = {
  native: true,
  chromeVisible: false,
  isAudio: false,
  playing: true,
  loading: false,
  subtitles: false,
  skipCard: false,
  upNext: false,
  introSkipped: false,
  stats: false,
  stillWatching: false,
};

describe("overlayKeptBy", () => {
  it("is empty only for a playing native film with nothing over it", () => {
    expect(overlayKeptBy(idle)).toBe("");
  });

  // Each of these draws over the picture, so hiding the overlay would hide it.
  it.each<[Partial<OverlayState>, string]>([
    [{ native: false }, "not native"],
    [{ chromeVisible: true }, "controls"],
    [{ isAudio: true }, "audio"],
    [{ playing: false }, "paused"],
    [{ loading: true }, "loading"],
    [{ subtitles: true }, "subtitles"],
    [{ skipCard: true }, "skip card"],
    [{ upNext: true }, "up next"],
    [{ introSkipped: true }, "intro skipped"],
    [{ stats: true }, "stats"],
    [{ stillWatching: true }, "still watching"],
  ])("%o keeps the overlay up", (change, reason) => {
    expect(overlayKeptBy({ ...idle, ...change })).toBe(reason);
  });
});

describe("nativeWake", () => {
  it("wakes, then hands the key to the player's keydown handler", () => {
    const order: string[] = [];
    const seen: KeyboardEvent[] = [];
    const onKey = (e: KeyboardEvent) => {
      order.push("key");
      seen.push(e);
    };
    window.addEventListener("keydown", onKey);
    try {
      nativeWake(() => order.push("wake"), { key: " ", code: "Space", shiftKey: false });
    } finally {
      window.removeEventListener("keydown", onKey);
    }
    expect(order).toEqual(["wake", "key"]);
    expect(seen[0].key).toBe(" ");
    expect(seen[0].code).toBe("Space");
  });

  it("only wakes for a mouse move or a key it could not name", () => {
    const onKey = vi.fn();
    const wake = vi.fn();
    window.addEventListener("keydown", onKey);
    try {
      nativeWake(wake, null);
      nativeWake(wake, undefined);
    } finally {
      window.removeEventListener("keydown", onKey);
    }
    expect(wake).toHaveBeenCalledTimes(2);
    expect(onKey).not.toHaveBeenCalled();
  });
});
