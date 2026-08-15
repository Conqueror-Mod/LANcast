import { beforeEach, describe, expect, it } from "vitest";
import { writeDevice } from "./device";
import {
  DEFAULT_BINDINGS,
  KEYS_STORAGE_KEY,
  bindingKeys,
  keyLabel,
  matchesBinding,
  mergeBindings,
  type KeyOverrides,
} from "./keys";
import { BIGSCREEN_KEY, bigscreenEnabled } from "./bigscreen";

beforeEach(() => {
  localStorage.clear();
  // The device store caches parsed values, so a test that only cleared
  // localStorage would read the previous test's answer.
  writeDevice<KeyOverrides>(KEYS_STORAGE_KEY, {});
  writeDevice<boolean>(BIGSCREEN_KEY, false);
});

describe("keybindings", () => {
  it("falls back to the defaults when nothing has been changed", () => {
    expect(bindingKeys("fullscreen")).toEqual(["f"]);
    expect(matchesBinding("help", "?")).toBe(true);
  });

  it("applies an override", () => {
    writeDevice<KeyOverrides>(KEYS_STORAGE_KEY, { fullscreen: ["v"] });
    expect(bindingKeys("fullscreen")).toEqual(["v"]);
    expect(matchesBinding("fullscreen", "f")).toBe(false);
  });

  /*
   * The reason overrides are stored rather than the whole map.
   *
   * Persisting every binding would freeze the set: a shortcut added in a later
   * version would be invisible to everybody who had ever opened the pane,
   * because their stored copy has no row for it and their stored copy is what
   * renders. This is that property, asserted — a stored override for a binding
   * that no longer exists must not resurrect it, and a binding with no stored
   * override must still appear.
   */
  it("takes new bindings from the defaults and ignores overrides for removed ones", () => {
    const merged = mergeBindings({ "a-shortcut-we-deleted": ["z"] });
    expect(merged).toHaveLength(DEFAULT_BINDINGS.length);
    expect(merged.some((b) => b.id === "a-shortcut-we-deleted")).toBe(false);
    expect(merged.find((b) => b.id === "mute")?.keys).toEqual(["m"]);
  });

  // Escape and the arrows are how you leave a screen and how you move between
  // tiles. A map that can strand you on a page you cannot escape is a trap, so
  // an override for a fixed binding is refused rather than merely hidden in the
  // UI — the storage is editable by hand.
  it("refuses to rebind a fixed shortcut even if the store says otherwise", () => {
    writeDevice<KeyOverrides>(KEYS_STORAGE_KEY, { back: ["q"], move: ["wasd"] });
    expect(bindingKeys("back")).toEqual(["Escape"]);
    expect(bindingKeys("move")).toEqual(["Arrows"]);
  });

  // An empty override is not a binding to nothing. Storing one would silently
  // delete a shortcut with no way back except the reset button somebody would
  // have to already know about.
  it("treats an empty override as no override", () => {
    writeDevice<KeyOverrides>(KEYS_STORAGE_KEY, { mute: [] });
    expect(bindingKeys("mute")).toEqual(["m"]);
  });

  it("labels keys the way somebody would say them", () => {
    expect(keyLabel(" ")).toBe("Space");
    expect(keyLabel("ArrowLeft")).toBe("←");
    expect(keyLabel("f")).toBe("F");
    expect(keyLabel("Ctrl+Shift+B")).toBe("Ctrl+Shift+B");
  });
});

describe("bigscreen", () => {
  it("is off until it is turned on, and persists once it is", () => {
    expect(bigscreenEnabled()).toBe(false);
    writeDevice<boolean>(BIGSCREEN_KEY, true);
    expect(bigscreenEnabled()).toBe(true);
    // The pre-paint script in index.html reads exactly this, so the stored
    // shape has to stay JSON a bare JSON.parse can read.
    expect(localStorage.getItem(BIGSCREEN_KEY)).toBe("true");
  });
});
