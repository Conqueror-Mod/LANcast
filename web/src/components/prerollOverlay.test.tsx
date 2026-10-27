/*
 * The pre-roll overlay (ADR 0099). Every way out starts the film, once, and the
 * player's own keys do not act on a film that has not started.
 */
import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { PrerollOverlay } from "./PrerollOverlay";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let done: number;

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  done = 0;
  act(() => {
    root.render(<PrerollOverlay src="/api/preroll/abc" onDone={() => done++} />);
  });
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

const video = () => host.querySelector("video")!;

describe("the pre-roll overlay", () => {
  it("plays the clip it was given", () => {
    expect(video().getAttribute("src")).toBe("/api/preroll/abc");
  });

  it("starts the film when the clip ends", () => {
    act(() => {
      video().dispatchEvent(new Event("ended"));
    });
    expect(done).toBe(1);
  });

  it("starts the film when the clip cannot play, without an error", () => {
    act(() => {
      video().dispatchEvent(new Event("error"));
    });
    expect(done).toBe(1);
    expect(host.textContent).not.toMatch(/error|can.t/i);
  });

  it("starts the film on Skip, and only once however it ends", () => {
    act(() => {
      (host.querySelector(".preroll__skip") as HTMLButtonElement).click();
    });
    act(() => {
      video().dispatchEvent(new Event("ended"));
    });
    expect(done).toBe(1);
  });

  it("skips on Escape and keeps the player's keys off the film", () => {
    let leaked = 0;
    const listener = () => leaked++;
    window.addEventListener("keydown", listener);
    act(() => {
      window.dispatchEvent(new KeyboardEvent("keydown", { key: " " }));
      window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    });
    window.removeEventListener("keydown", listener);
    expect(leaked).toBe(0);
    expect(done).toBe(1);
  });
});
