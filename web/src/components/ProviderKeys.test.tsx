/*
 * Settings › Metadata's key rows (ADR 0110): each says which key is in use,
 * the built-in switch appears only in a build that carries a key, and OMDb
 * points at where to get one.
 */
import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import type { Settings, SettingsUpdate } from "@/api/types";
import { ProviderKeys, keyStatus, OMDB_KEY_URL } from "./ProviderKeys";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let saves: SettingsUpdate[];

beforeEach(() => {
  saves = [];
  host = document.createElement("div");
  document.body.appendChild(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

function settings(over: Partial<Settings> = {}): Settings {
  return {
    tmdb: { configured: false, source: "built_in", built_in: true },
    opensubtitles: { configured: false, source: "built_in", built_in: true, username: "" },
    omdb: { configured: false },
    builtin_keys_off: false,
    ...over,
  } as Settings;
}

function render(s: Settings) {
  act(() => root.render(<ProviderKeys settings={s} pending={false} save={(p) => saves.push(p)} />));
}

function toggle(): HTMLInputElement | undefined {
  return [...host.querySelectorAll("label")]
    .find((l) => l.textContent?.includes("built-in keys"))
    ?.querySelector("input") ?? undefined;
}

describe("keyStatus", () => {
  it("names the key in use", () => {
    expect(keyStatus(true, "own")).toBe("Using your key");
    expect(keyStatus(false, "built_in")).toBe("Using LANcast's built-in key");
    expect(keyStatus(false, "none")).toBe("Not set");
  });
});

describe("ProviderKeys", () => {
  it("says the built-in key is in use and offers the switch", () => {
    render(settings());
    expect(host.textContent).toContain("Using LANcast's built-in key");
    expect(toggle()?.checked).toBe(true);
  });

  it("switching off saves builtin_keys_off: true", () => {
    render(settings());
    act(() => toggle()!.click());
    expect(saves).toEqual([{ builtin_keys_off: true }]);
  });

  it("hides the switch in a build with no built-in keys", () => {
    render(
      settings({
        tmdb: { configured: false, source: "none", built_in: false },
        opensubtitles: { configured: false, source: "none", built_in: false, username: "" },
      }),
    );
    expect(toggle()).toBeUndefined();
    expect(host.textContent).toContain("Not set");
  });

  it("links OMDb to where a key is got", () => {
    render(settings());
    const link = [...host.querySelectorAll("a")].find((a) => a.href === OMDB_KEY_URL);
    expect(link).toBeDefined();
    expect(link?.target).toBe("_blank");
  });

  it("offers Remove for your own key, which saves an empty key", () => {
    render(settings({ tmdb: { configured: true, source: "own", built_in: true } }));
    const remove = [...host.querySelectorAll("button")].find((b) => b.textContent === "Remove");
    act(() => remove!.click());
    expect(saves).toEqual([{ tmdb_key: "" }]);
  });

  it("shows the OpenSubtitles account by name and removes it by clearing the name", () => {
    render(
      settings({
        opensubtitles: { configured: false, source: "built_in", built_in: true, username: "somebody" },
      }),
    );
    expect(host.textContent).toContain("somebody's daily allowance");
    const remove = [...host.querySelectorAll("button")].find((b) => b.textContent === "Remove");
    act(() => remove!.click());
    expect(saves).toEqual([{ opensubtitles_username: "" }]);
  });

  it("offers no login when subtitle search is off", () => {
    render(
      settings({
        builtin_keys_off: true,
        tmdb: { configured: false, source: "none", built_in: true },
        opensubtitles: { configured: false, source: "none", built_in: true, username: "" },
      }),
    );
    expect(host.querySelector('[aria-label="OpenSubtitles username"]')).toBeNull();
  });
});
