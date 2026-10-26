/*
 * The browser player's states panel (RetroWebPlay.tsx, ADR 0077).
 *
 * The browser and the desktop app save to the same three slots in formats
 * neither can load from the other. So what the panel must get right is which
 * slots it offers to load, and that it says who made a state it cannot load
 * before somebody saves over it.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { StatesPanel } from "./RetroWebPlay";

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

const OURS = "retroarch-web/mgba";

function mount(stored: { slot: string; core?: string }[]) {
  const onSave = vi.fn();
  const onLoad = vi.fn();
  const onClose = vi.fn();
  act(() =>
    root.render(
      <MemoryRouter>
        <FocusProvider>
          <StatesPanel stored={stored} ours={OURS} onSave={onSave} onLoad={onLoad} onClose={onClose} />
        </FocusProvider>
      </MemoryRouter>,
    ),
  );
  return { onSave, onLoad, onClose };
}

const row = (slot: string) => host.querySelector(`[data-slot="${slot}"]`)!;
const labels = (el: Element) => [...el.querySelectorAll("button")].map((b) => b.textContent?.trim());

describe("the browser's states panel", () => {
  it("offers all three slots to save to, and none to load when nothing is saved", () => {
    mount([]);
    for (const n of [1, 2, 3]) {
      expect(labels(row(`state-${n}`))).toEqual([`Save to Slot ${n}`]);
      expect(row(`state-${n}`).textContent).toContain("Empty");
    }
  });

  it("offers to load a state this browser made, and saves to its slot", () => {
    const { onSave, onLoad } = mount([{ slot: "state-2", core: OURS }]);
    expect(labels(row("state-2"))).toEqual(["Save to Slot 2", "Load Slot 2"]);
    act(() => (row("state-2").querySelectorAll("button")[1] as HTMLButtonElement).click());
    expect(onLoad).toHaveBeenCalledWith("state-2");
    act(() => (row("state-2").querySelectorAll("button")[0] as HTMLButtonElement).click());
    expect(onSave).toHaveBeenCalledWith("state-2");
  });

  it("says a desktop state cannot load here, offers no load, and names saving over it", () => {
    mount([{ slot: "state-1", core: "mgba" }]);
    expect(row("state-1").textContent).toContain("Made in the desktop app, which a browser can't load");
    expect(labels(row("state-1"))).toEqual(["Save over Slot 1"]);
  });

  it("does not offer another emulator's browser state either", () => {
    mount([{ slot: "state-3", core: "retroarch-web/gearsystem" }]);
    expect(labels(row("state-3"))).toEqual(["Save over Slot 3"]);
    expect(row("state-3").textContent).toContain("another emulator");
  });

  // The game's own save and the auto state are not numbered slots.
  it("lists only the numbered slots", () => {
    mount([
      { slot: "sram" },
      { slot: "auto", core: OURS },
    ]);
    expect(host.querySelectorAll("[data-slot]").length).toBe(3);
    expect(host.textContent).not.toContain("Where you left off");
  });

  it("goes back to the game from its own button", () => {
    const { onClose } = mount([]);
    const back = [...host.querySelectorAll("button")].find((b) => b.textContent === "Back to the game")!;
    act(() => back.click());
    expect(onClose).toHaveBeenCalled();
  });
});
