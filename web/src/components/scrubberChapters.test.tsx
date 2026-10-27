/*
 * Chapters on the scrub bar. jsdom performs no layout, so the track's width is
 * stubbed for the hover test; what this proves is which ticks are drawn where,
 * and which chapter is named, not how it looks.
 */
import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { Scrubber } from "./Scrubber";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;

const chapters = [
  { start_ms: 0, end_ms: 300_000, title: "Opening" },
  { start_ms: 300_000, end_ms: 900_000, title: "The Heist" },
  { start_ms: 900_000, end_ms: 1_200_000, title: "Credits" },
];

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

function render(withChapters = chapters) {
  act(() => {
    root.render(<Scrubber current={60} duration={1200} onSeek={() => {}} chapters={withChapters} />);
  });
}

describe("chapter marks", () => {
  it("draws a tick at each chapter start after the first", () => {
    render();
    const marks = [...host.querySelectorAll<HTMLElement>('[data-testid="chapter-mark"]')];
    expect(marks.map((m) => m.style.left)).toEqual(["25%", "75%"]);
  });

  it("draws nothing for a film without chapters", () => {
    render([]);
    expect(host.querySelectorAll('[data-testid="chapter-mark"]').length).toBe(0);
    expect(host.querySelector('[role="slider"]')?.getAttribute("aria-valuetext")).toBeNull();
  });

  it("says which chapter is playing to a screen reader", () => {
    render();
    expect(host.querySelector('[role="slider"]')?.getAttribute("aria-valuetext")).toContain("Opening");
  });

  it("names the chapter under the pointer", () => {
    render();
    const slider = host.querySelector<HTMLElement>('[role="slider"]')!;
    slider.getBoundingClientRect = () => ({ left: 0, width: 1000, top: 0, height: 10, right: 1000, bottom: 10, x: 0, y: 0, toJSON() {} });
    // jsdom has no PointerEvent; React reads clientX off any event of the type.
    act(() => {
      slider.dispatchEvent(new MouseEvent("pointermove", { bubbles: true, clientX: 500 }));
    });
    expect(host.querySelector(".scrubber__label")?.textContent).toBe("The Heist");
    // React derives pointer-leave from pointerout with somewhere else to go.
    act(() => {
      slider.dispatchEvent(new MouseEvent("pointerout", { bubbles: true, relatedTarget: document.body }));
    });
    expect(host.querySelector(".scrubber__label")).toBeNull();
  });
});
