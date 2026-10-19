/*
 * The quality ceiling as a request, and the preferences it is stored in.
 *
 * The one thing here that is not obvious by reading is the empty string. A
 * request naming no ceiling has to be byte-for-byte the request this client
 * made before quality selection existed, because that is what makes "Original"
 * mean the same thing as "no opinion" — and the server answers a capped request
 * differently even when the cap is generous. A `max_height=0` slipping into the
 * URL would be a ceiling of nothing, and the server reads 0 as no limit, so the
 * bug would be invisible in behaviour and visible only as a different decision
 * path being taken on every single playback.
 */
import { describe, it, expect, beforeEach, vi } from "vitest";
import {
  cycleStage,
  prefsForVizKey,
  resolveMilkdrop,
  vizChoices,
  vizKeyOf,
  stageTier,
  vizStages,
  QUALITIES,
  DEFAULTS,
  qualityByID,
  qualityQuery,
  getPrefs,
  setPrefs,
  resetPrefs,
} from "./prefs";

beforeEach(() => {
  localStorage.clear();
  resetPrefs();
});

describe("quality ladder", () => {
  it("has Original first, and Original is the default", () => {
    expect(QUALITIES[0].id).toBe("original");
    expect(DEFAULTS.quality).toBe("original");
  });

  it("asks for nothing at Original", () => {
    expect(qualityQuery("original")).toBe("");
  });

  it("pairs a height with a bitrate on every capped rung", () => {
    // Offering them separately makes "1080p at 1 Mbps" reachable, which is a
    // worse picture than 480p at the same rate and reads as a broken server.
    for (const q of QUALITIES.slice(1)) {
      expect(q.height, q.id).toBeGreaterThan(0);
      expect(q.bitrate, q.id).toBeGreaterThan(0);
    }
  });

  it("sends both limits, in the units the server reads", () => {
    // Bits per second, matching probe.Profile. Kilobits here would be a
    // thousand-fold cap and a black rectangle.
    expect(qualityQuery("720p4")).toBe("max_height=720&max_bitrate=4000000");
  });

  it("falls back to Original for an id it does not know", () => {
    // A stored preference outlives the build that wrote it; a removed rung must
    // not leave the player asking for a ceiling nothing can resolve.
    expect(qualityByID("2160p-ultra").id).toBe("original");
    expect(qualityQuery("2160p-ultra")).toBe("");
  });
});

describe("stored preferences", () => {
  it("round-trips through storage", () => {
    setPrefs({ subSize: 1.35, autoPlay: false });
    expect(JSON.parse(localStorage.getItem("lancast:playback-prefs")!)).toMatchObject({
      subSize: 1.35,
      autoPlay: false,
    });
  });

  // Storage is read once, at import. Both of these are about *that* read, so
  // they reset the module and import it again with storage already primed —
  // which is the shape of the real case: a browser that has an old object in
  // storage before this code ever runs.
  it("fills in a preference the stored object predates", async () => {
    // A build that adds a setting finds every older browser's stored object
    // missing it. `undefined` reaching a <select value> renders blank, and
    // reaching a CSS variable renders nothing at all.
    localStorage.setItem(
      "lancast:playback-prefs",
      JSON.stringify({ quality: "720p4" }),
    );
    vi.resetModules();
    const fresh = await import("./prefs");
    expect(fresh.getPrefs().quality).toBe("720p4");
    expect(fresh.getPrefs().subSize).toBe(DEFAULTS.subSize);
    expect(fresh.getPrefs().autoPlay).toBe(DEFAULTS.autoPlay);
  });

  it("survives unreadable storage", async () => {
    localStorage.setItem("lancast:playback-prefs", "{not json");
    vi.resetModules();
    // Garbage must give the defaults rather than throwing on the way into the
    // provider, which would take the whole player down at mount.
    const fresh = await import("./prefs");
    expect(fresh.getPrefs()).toEqual(DEFAULTS);
  });

  it("notifies every reader, so the panel and the provider cannot disagree", () => {
    const seen: string[] = [];
    // The store is what stops the panel ticking a quality the provider is not
    // streaming at.
    setPrefs({ quality: "720p2" });
    seen.push(getPrefs().quality);
    setPrefs({ quality: "original" });
    seen.push(getPrefs().quality);
    expect(seen).toEqual(["720p2", "original"]);
  });
});

describe("the stage's visualizers", () => {
  it("offers the classic pair everywhere and the trails only with WebGL2", () => {
    expect(vizStages(false)).toEqual(["spectrum", "scope"]);
    expect(vizStages(true).slice(0, 2)).toEqual(["spectrum", "scope"]);
    expect(vizStages(true).length).toBeGreaterThanOrEqual(6);
    expect(vizStages(true)).not.toContain("cover");
  });

  it("steps forward and back round the offered list, never onto the cover", () => {
    const offered = vizStages(true);
    expect(cycleStage("spectrum", 1, offered)).toBe("scope");
    expect(cycleStage("spectrum", -1, offered)).toBe(offered[offered.length - 1]);
    expect(cycleStage(offered[offered.length - 1], 1, offered)).toBe("spectrum");
  });

  it("steps from the cover to the first or last visualizer", () => {
    const offered = vizStages(false);
    expect(cycleStage("cover", 1, offered)).toBe("spectrum");
    expect(cycleStage("cover", -1, offered)).toBe("scope");
  });

  it("steps out of a trail this screen cannot offer onto one it can", () => {
    expect(cycleStage("tunnel", 1, vizStages(false))).toBe("spectrum");
  });

  it("gives each stage its tier, and the cover none", () => {
    expect(stageTier("cover")).toBe(0);
    expect(stageTier("scope")).toBe(1);
    expect(stageTier("smoke")).toBe(2);
  });
});

describe("the MilkDrop stage", () => {
  const ids = ["a1", "b2", "c3"];

  it("is tier 3, after the trails", () => {
    expect(stageTier("milkdrop")).toBe(3);
  });

  it("offers each stored preset as its own stop, after the built-ins, and only with WebGL2", () => {
    expect(vizChoices(true, ids).slice(-3)).toEqual(["milkdrop:a1", "milkdrop:b2", "milkdrop:c3"]);
    expect(vizChoices(true, ids).slice(0, vizStages(true).length)).toEqual(vizStages(true));
    expect(vizChoices(false, ids)).toEqual(vizStages(false));
    expect(vizChoices(true, [])).toEqual(vizStages(true));
  });

  it("steps from the last trail into the first preset, and between presets", () => {
    const offered = vizChoices(true, ids);
    const lastTrail = vizStages(true)[vizStages(true).length - 1];
    expect(cycleStage(lastTrail, 1, offered)).toBe("milkdrop:a1");
    expect(prefsForVizKey(cycleStage("milkdrop:a1", 1, offered))).toEqual({ stage: "milkdrop", milkdrop: "b2" });
    expect(prefsForVizKey(cycleStage("milkdrop:c3", 1, offered))).toEqual({ stage: "spectrum" });
  });

  it("names the preset showing, and the first when the remembered one is gone", () => {
    expect(vizKeyOf({ stage: "milkdrop", milkdrop: "b2" }, ids)).toBe("milkdrop:b2");
    expect(vizKeyOf({ stage: "milkdrop", milkdrop: "deleted" }, ids)).toBe("milkdrop:a1");
    expect(vizKeyOf({ stage: "scope", milkdrop: "b2" }, ids)).toBe("scope");
    expect(resolveMilkdrop("x", [])).toBe("");
  });
});
