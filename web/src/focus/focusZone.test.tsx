/*
 * Focus zones: a form whose native controls arrows and a remote can reach
 * without each one registering by hand (Settings, settings-3-plan §8).
 *
 * jsdom performs no layout, so every control is given a position: a nav column
 * on the left (x 0) and a pane on the right (x 400), the shape of Settings.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act, useState } from "react";
import { createRoot, type Root } from "react-dom/client";
import { FocusProvider, useFocusZone } from "./FocusController";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

const at = (x: number, y: number) => (el: HTMLElement | null) => {
  if (el) el.getBoundingClientRect = () => new DOMRect(x, y, 120, 30);
};

let addRow: () => void = () => {};

function Form({ zoned = true }: { zoned?: boolean }) {
  const zone = useFocusZone();
  const [extra, setExtra] = useState(false);
  addRow = () => setExtra(true);
  return (
    <div ref={zoned ? zone : undefined}>
      <div data-focus-column>
        <button ref={at(0, 100)}>Video</button>
        <button ref={at(0, 150)}>Music</button>
        {/* Low in the nav, below the pane's last row: Down must not reach it from the pane. */}
        <button ref={at(0, 900)}>About</button>
      </div>
      <div data-focus-column>
      <select ref={at(400, 100)} aria-label="Quality" defaultValue="a">
        <option value="a">Original</option>
        <option value="b">720p</option>
      </select>
      <input ref={at(400, 150)} aria-label="Name" defaultValue="Den" />
      <input ref={at(400, 200)} type="checkbox" aria-label="Autoplay" />
      <button ref={at(400, 250)} disabled>
        Busy
      </button>
      <button ref={at(400, 300)}>Save</button>
      {extra && <button ref={at(400, 350)}>Added later</button>}
      </div>
    </div>
  );
}

async function render(zoned = true) {
  await act(async () => {
    root.render(
      <FocusProvider>
        <Form zoned={zoned} />
      </FocusProvider>,
    );
  });
}

const byLabel = (label: string) =>
  [...host.querySelectorAll<HTMLElement>("button, select, input")].find(
    (e) => e.textContent === label || e.getAttribute("aria-label") === label,
  )!;

function key(k: string) {
  act(() => {
    document.dispatchEvent(new KeyboardEvent("keydown", { key: k, bubbles: true, cancelable: true }));
  });
}

describe("a focus zone", () => {
  it("is the fix: without one, arrows go nowhere in the form", async () => {
    await render(false);
    act(() => byLabel("Video").focus());
    key("ArrowDown");
    expect(document.activeElement).toBe(byLabel("Video"));
  });

  it("moves down the nav and across into the pane", async () => {
    await render();
    act(() => byLabel("Video").focus());
    key("ArrowDown");
    expect(document.activeElement).toBe(byLabel("Music"));
    key("ArrowUp");
    key("ArrowRight");
    expect(document.activeElement).toBe(byLabel("Quality"));
    key("ArrowLeft");
    expect(document.activeElement).toBe(byLabel("Video"));
  });

  it("lets a remote leave a dropdown, and opens it on Enter", async () => {
    await render();
    const quality = byLabel("Quality") as HTMLSelectElement;
    const picker = vi.fn();
    quality.showPicker = picker;
    act(() => quality.focus());
    key("Enter");
    expect(picker).toHaveBeenCalledOnce();
    key("ArrowDown");
    expect(document.activeElement).toBe(byLabel("Name"));
    expect(quality.value).toBe("a");
  });

  it("keeps left and right for a text box's cursor, and up and down for moving", async () => {
    await render();
    act(() => byLabel("Name").focus());
    key("ArrowLeft");
    expect(document.activeElement).toBe(byLabel("Name"));
    key("ArrowDown");
    expect(document.activeElement).toBe(byLabel("Autoplay"));
  });

  it("flips a switch on Enter, which is what a remote's OK sends", async () => {
    await render();
    const box = byLabel("Autoplay") as HTMLInputElement;
    act(() => box.focus());
    key("Enter");
    expect(box.checked).toBe(true);
  });

  it("steps over a disabled control rather than stopping at it", async () => {
    await render();
    act(() => byLabel("Autoplay").focus());
    key("ArrowDown");
    expect(document.activeElement).toBe(byLabel("Save"));
  });

  it("reaches a control that appears after the zone was set up", async () => {
    await render();
    await act(async () => addRow());
    // The zone registers on a MutationObserver callback, a microtask later.
    await act(async () => {});
    act(() => byLabel("Save").focus());
    key("ArrowDown");
    expect(document.activeElement).toBe(byLabel("Added later"));
  });

  it("keeps up and down inside a column, so the pane's last row is not a door into the nav", async () => {
    await render();
    act(() => byLabel("Save").focus());
    key("ArrowDown");
    expect(document.activeElement).toBe(byLabel("Save"));
    key("ArrowLeft");
    expect(document.activeElement?.textContent).toMatch(/Music|Video|About/);
  });

  it("leaves the form's Tab order alone", async () => {
    await render();
    for (const label of ["Video", "Music", "Quality", "Name", "Autoplay", "Save"]) {
      expect(byLabel(label).tabIndex, label).toBe(0);
    }
  });
});
