/*
 * Previews on the scrub bar (ADR 0105). jsdom performs no layout, so the
 * track's width is stubbed; what this proves is which sheet is asked for and
 * which window of it is shown, not how it looks.
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

const sheets = { interval_ms: 10_000, columns: 10, rows: 10, width: 240, height: 136, sheets: 3 };

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

function render(previews: Parameters<typeof Scrubber>[0]["previews"]) {
  act(() => {
    root.render(<Scrubber current={0} duration={2500} onSeek={() => {}} previews={previews} />);
  });
  const slider = host.querySelector<HTMLElement>('[role="slider"]')!;
  slider.getBoundingClientRect = () => ({
    left: 0, width: 1000, top: 0, height: 10, right: 1000, bottom: 10, x: 0, y: 0, toJSON() {},
  });
  return slider;
}

const hoverAt = (slider: HTMLElement, x: number) =>
  act(() => {
    slider.dispatchEvent(new MouseEvent("pointermove", { bubbles: true, clientX: x }));
  });

describe("previews on the scrub bar", () => {
  it("shows the frame under the pointer, from the sheet that holds it", () => {
    const slider = render({ sheets, url: (n) => `/api/items/7/previews/${n}` });
    expect(host.querySelector('[data-testid="scrub-preview"]')).toBeNull();

    // 600 px of 1,000 over 2,500 s is 1,500 s: frame 150, sheet 1, row 5, column 0.
    hoverAt(slider, 600);
    const frame = host.querySelector<HTMLElement>('[data-testid="scrub-preview"]')!;
    expect(frame.style.backgroundImage).toBe('url("/api/items/7/previews/1")');
    expect(frame.style.backgroundPosition).toBe("0px -680px");
    expect(frame.style.width).toBe("240px");

    // Moving on moves the window, on the same sheet.
    hoverAt(slider, 612); // 1,530 s: frame 153, column 3
    expect(frame.style.backgroundImage).toBe('url("/api/items/7/previews/1")');
    expect(frame.style.backgroundPosition).toBe("-720px -680px");
  });

  it("shows nothing without previews, as before they existed", () => {
    const slider = render(null);
    hoverAt(slider, 600);
    expect(host.querySelector('[data-testid="scrub-preview"]')).toBeNull();
  });
});
