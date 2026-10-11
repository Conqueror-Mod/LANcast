import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  PHONE_MAX_WIDTH,
  TILE_DEFAULT_STEP,
  TILE_SIZE_KEY,
  TILE_STEPS,
  clampStep,
  defaultStep,
  tileWidth,
} from "./tileSize";
import { readDevice, writeDevice } from "./device";

beforeEach(() => {
  localStorage.clear();
});

describe("tile size steps", () => {
  it("keeps the historical grid width as the default", () => {
    // 160px is what --tile-grid has always been. A default that quietly
    // resizes every existing library on upgrade is a migration, not a setting.
    expect(TILE_STEPS[TILE_DEFAULT_STEP]).toBe(160);
  });

  it("orders the steps smallest first", () => {
    const sorted = [...TILE_STEPS].sort((a, b) => a - b);
    expect([...TILE_STEPS]).toEqual(sorted);
  });
});

describe("clampStep", () => {
  it("passes a valid step through", () => {
    expect(clampStep(0)).toBe(0);
    expect(clampStep(TILE_STEPS.length - 1)).toBe(TILE_STEPS.length - 1);
  });

  it("clamps a step from a build with more notches", () => {
    expect(clampStep(99)).toBe(TILE_STEPS.length - 1);
    expect(clampStep(-4)).toBe(0);
  });

  it("falls back to the default for a value that is not a number", () => {
    // A hand-edited or corrupt store must not reach the grid: `minmax(undefined,
    // 1fr)` collapses the layout and reads as lost data rather than as a bad
    // preference.
    for (const bad of [undefined, null, NaN, "3", {}]) {
      expect(clampStep(bad)).toBe(TILE_DEFAULT_STEP);
    }
  });
});

describe("tileWidth", () => {
  it("renders a px width for every step", () => {
    TILE_STEPS.forEach((px, i) => expect(tileWidth(i)).toBe(`${px}px`));
  });

  it("renders the default width for junk", () => {
    expect(tileWidth("nonsense")).toBe(`${TILE_STEPS[TILE_DEFAULT_STEP]}px`);
  });
});

describe("persistence", () => {
  it("survives a reload through the device store", () => {
    writeDevice(TILE_SIZE_KEY, 4);
    expect(JSON.parse(localStorage.getItem(TILE_SIZE_KEY)!)).toBe(4);
    expect(readDevice(TILE_SIZE_KEY, TILE_DEFAULT_STEP)).toBe(4);
  });
});

/*
 * The phone default. At 160px a phone showed one poster per row and the
 * first one below the fold; an unset preference on a narrow screen starts at
 * the smallest step instead. jsdom has no matchMedia, so it is stubbed with
 * the answer each case needs.
 */
describe("the default on a phone", () => {
  afterEach(() => vi.unstubAllGlobals());

  const screen = (narrow: boolean) =>
    vi.stubGlobal("matchMedia", (q: string) => ({
      matches: narrow && q === `(max-width: ${PHONE_MAX_WIDTH}px)`,
    }));

  it("starts a narrow screen at the smallest tiles", () => {
    screen(true);
    expect(TILE_STEPS[defaultStep()]).toBe(96);
  });

  it("leaves a wide screen at the historical default", () => {
    screen(false);
    expect(defaultStep()).toBe(TILE_DEFAULT_STEP);
  });
});
