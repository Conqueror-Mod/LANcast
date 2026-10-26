/*
 * The phone's on-screen controls on the page (RetroTouchControls.tsx).
 *
 * What matters is what reaches the game: a key held for as long as a thumb is
 * down, let go when it lifts, and never left down behind a touch the browser
 * cancelled. jsdom lays nothing out, so where the controls sit in portrait
 * and landscape is checked in a browser, not here.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { RetroTouchControls, useGamepadConnected } from "./RetroTouchControls";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let canvas: HTMLCanvasElement;
let keys: string[];

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  canvas = document.createElement("canvas");
  document.body.append(canvas);
  keys = [];
  const log = (e: KeyboardEvent) => keys.push(`${e.type === "keydown" ? "+" : "-"}${e.code}`);
  canvas.addEventListener("keydown", log);
  canvas.addEventListener("keyup", log);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  canvas.remove();
  vi.restoreAllMocks();
});

function mount(platform: string) {
  act(() => root.render(<RetroTouchControls platform={platform} target={() => canvas} />));
}

// jsdom has no PointerEvent constructor; React reads what it needs from a MouseEvent.
function pointer(el: Element, type: string, x = 0, y = 0) {
  act(() => {
    el.dispatchEvent(new MouseEvent(type, { bubbles: true, clientX: x, clientY: y, buttons: type === "pointerup" ? 0 : 1 }));
  });
}

const key = (button: string) => host.querySelector(`[data-button="${button}"]`)!;

describe("the on-screen controls", () => {
  it("holds a button's key from the touch landing to the touch lifting", () => {
    mount("nes");
    pointer(key("a"), "pointerdown");
    expect(keys).toEqual(["+KeyX"]);
    pointer(key("a"), "pointerup");
    expect(keys).toEqual(["+KeyX", "-KeyX"]);
  });

  it("lets go when the browser cancels the touch", () => {
    mount("nes");
    pointer(key("start"), "pointerdown");
    pointer(key("start"), "pointercancel");
    expect(keys).toEqual(["+Enter", "-Enter"]);
  });

  it("lets two thumbs hold two buttons at once", () => {
    mount("gba");
    pointer(key("b"), "pointerdown");
    pointer(key("r"), "pointerdown");
    expect(keys).toEqual(["+KeyZ", "+KeyW"]);
    pointer(key("b"), "pointerup");
    expect(keys).toEqual(["+KeyZ", "+KeyW", "-KeyZ"]);
  });

  it("steers with the d-pad as the thumb slides, and lets go of everything when it lifts", () => {
    mount("nes");
    const dpad = host.querySelector('[data-testid="dpad"]')!;
    vi.spyOn(dpad, "getBoundingClientRect").mockReturnValue({
      left: 0, top: 0, width: 100, height: 100, right: 100, bottom: 100, x: 0, y: 0, toJSON: () => ({}),
    } as DOMRect);
    pointer(dpad, "pointerdown", 95, 50); // right
    expect(keys).toEqual(["+ArrowRight"]);
    pointer(dpad, "pointermove", 85, 15); // up-right
    expect(keys).toEqual(["+ArrowRight", "+ArrowUp"]);
    pointer(dpad, "pointermove", 50, 5); // up
    expect(keys).toEqual(["+ArrowRight", "+ArrowUp", "-ArrowRight"]);
    pointer(dpad, "pointerup", 50, 5);
    expect(keys).toEqual(["+ArrowRight", "+ArrowUp", "-ArrowRight", "-ArrowUp"]);
  });

  // A key left down walks the character off a ledge with nobody touching.
  it("lets go of every held key when the window loses focus", () => {
    mount("nes");
    pointer(key("a"), "pointerdown");
    pointer(key("start"), "pointerdown");
    act(() => void window.dispatchEvent(new Event("blur")));
    expect(keys).toEqual(["+KeyX", "+Enter", "-KeyX", "-Enter"]);
  });

  it("lets go of every held key when it goes away", () => {
    mount("nes");
    pointer(key("b"), "pointerdown");
    act(() => root.unmount());
    root = createRoot(host);
    expect(keys).toEqual(["+KeyZ", "-KeyZ"]);
  });

  // The game screen hands a new target function on every render. If that
  // reset the controls, a notice appearing would let go of a held button.
  it("keeps a button held while the game screen re-renders", () => {
    mount("nes");
    pointer(key("a"), "pointerdown");
    act(() => root.render(<RetroTouchControls platform="nes" target={() => canvas} />));
    act(() => root.render(<RetroTouchControls platform="nes" target={() => canvas} />));
    expect(keys).toEqual(["+KeyX"]);
  });

  it("shows the console's own buttons", () => {
    mount("genesis");
    const names = [...host.querySelectorAll(".rtouch__key")].map((b) => b.textContent);
    expect(names).toEqual(["Start", "A", "B", "C"]);
  });

  it("is touch only: no button takes keyboard focus", () => {
    mount("gba");
    for (const b of host.querySelectorAll("button")) expect(b.tabIndex).toBe(-1);
  });
});

describe("useGamepadConnected", () => {
  function Probe() {
    return <span data-pad={String(useGamepadConnected())} />;
  }

  it("follows a controller arriving and leaving", () => {
    let pads: (Gamepad | null)[] = [];
    // jsdom has no Gamepad API at all, so it is put there for this test.
    Object.defineProperty(navigator, "getGamepads", { configurable: true, value: () => pads });
    act(() => root.render(<Probe />));
    const state = () => host.querySelector("[data-pad]")!.getAttribute("data-pad");
    expect(state()).toBe("false");
    pads = [{} as Gamepad];
    act(() => void window.dispatchEvent(new Event("gamepadconnected")));
    expect(state()).toBe("true");
    pads = [null];
    act(() => void window.dispatchEvent(new Event("gamepaddisconnected")));
    expect(state()).toBe("false");
    delete (navigator as unknown as { getGamepads?: unknown }).getGamepads;
  });
});
