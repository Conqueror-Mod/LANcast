/*
 * Up Next for an episode with no credits marker, and a season's queue running
 * on into the next season: the two halves of "the finale stopped dead"
 * (Ash vs Evil Dead S1E10, 2026-11-22).
 */
import { describe, it, expect } from "vitest";
import {
  CREDITS_LEAD_SECONDS,
  UNMARKED_UP_NEXT_SECONDS,
  upNextWithoutMarker,
} from "./skip";
import { seasonQueue } from "./seasonQueue";
import { UP_NEXT_SECONDS } from "@/components/UpNextCard";
import type { components } from "@/api/schema";

type Marker = components["schemas"]["Marker"];

const credits = (startS: number, source = "blackdetect"): Marker => ({
  kind: "credits",
  start_ms: startS * 1000,
  source,
  confidence: 0.9,
  created_at: 0,
});

// S1E10's length.
const EP = 2091;

describe("Up Next without a credits marker", () => {
  it("runs its count out where a roll-on lands, so nothing is cut", () => {
    expect(UNMARKED_UP_NEXT_SECONDS).toBe(UP_NEXT_SECONDS + CREDITS_LEAD_SECONDS);
  });

  it("comes up for an episode's last seconds and not before", () => {
    const from = EP - UNMARKED_UP_NEXT_SECONDS;
    expect(upNextWithoutMarker([], from - 1, EP, "episode")).toBe(false);
    expect(upNextWithoutMarker([], from, EP, "episode")).toBe(true);
    expect(upNextWithoutMarker(undefined, from + 5, EP, "episode")).toBe(true);
    // Past the lead the ending itself has it.
    expect(upNextWithoutMarker([], EP - CREDITS_LEAD_SECONDS, EP, "episode")).toBe(false);
  });

  it("leaves an episode with a believed marker to the marker", () => {
    expect(upNextWithoutMarker([credits(EP * 0.95)], EP - 5, EP, "episode")).toBe(false);
  });

  it("stands in for a marker the player does not believe", () => {
    // At 50%: outside the window, so there is no Skip credits to replace.
    expect(upNextWithoutMarker([credits(EP * 0.5)], EP - 5, EP, "episode")).toBe(true);
  });

  it("is for episodes only, and needs a length", () => {
    expect(upNextWithoutMarker([], EP - 5, EP, "movie")).toBe(false);
    expect(upNextWithoutMarker([], 5, 0, "episode")).toBe(false);
    expect(upNextWithoutMarker([], 5, NaN, "episode")).toBe(false);
  });
});

describe("a season's queue", () => {
  const show = [1, 2, 3, 4, 5, 6];

  it("runs on through the rest of the show", () => {
    expect(seasonQueue([1, 2, 3], show)).toEqual([1, 2, 3, 4, 5, 6]);
    expect(seasonQueue([4, 5, 6], show)).toEqual([4, 5, 6]);
  });

  it("never reaches back into an earlier season", () => {
    expect(seasonQueue([4, 5], show)[0]).toBe(4);
  });

  it("is the season alone until the show's list arrives", () => {
    expect(seasonQueue([1, 2, 3], undefined)).toEqual([1, 2, 3]);
  });

  it("is the season alone when the two lists disagree", () => {
    // An episode on this page missing from the show's list: running on from
    // the show's list would skip it.
    expect(seasonQueue([1, 2, 3], [1, 3, 4, 5])).toEqual([1, 2, 3]);
    expect(seasonQueue([9], show)).toEqual([9]);
    expect(seasonQueue([], show)).toEqual([]);
  });
});
