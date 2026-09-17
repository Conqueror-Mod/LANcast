/*
 * The player-backend seam (ADR 0067, Phase 1).
 *
 * The one behaviour worth pinning is the one that would be invisible if it
 * broke: the provider's handlers used to be JSX props, which always see the
 * latest render. A listener bound once would see the first render's closure
 * for ever, and nothing would fail — the clock would just be wrong.
 */
import { describe, it, expect } from "vitest";
import { attachMediaHandlers, MEDIA_EVENTS, type MediaBackend } from "./backend";

describe("attachMediaHandlers", () => {
  it("calls the handler table as it is when the event fires, not when attached", () => {
    const v = document.createElement("video");
    const seen: string[] = [];
    let table: Record<string, (m: MediaBackend) => void> = {
      timeupdate: () => seen.push("first render"),
    };
    attachMediaHandlers(v, () => table);

    table = { timeupdate: () => seen.push("latest render") };
    v.dispatchEvent(new Event("timeupdate"));

    expect(seen).toEqual(["latest render"]);
  });

  it("hands the backend to the handler, which for html5 is the element", () => {
    const v = document.createElement("video");
    let got: MediaBackend | null = null;
    attachMediaHandlers(v, () => ({ play: (m) => (got = m) }));
    v.dispatchEvent(new Event("play"));
    expect(got).toBe(v);
  });

  it("detaches every event it attached", () => {
    const v = document.createElement("video");
    let calls = 0;
    const handlers = Object.fromEntries(MEDIA_EVENTS.map((n) => [n, () => calls++]));
    const detach = attachMediaHandlers(v, () => handlers);
    detach();
    for (const n of MEDIA_EVENTS) v.dispatchEvent(new Event(n));
    expect(calls).toBe(0);
  });
});
