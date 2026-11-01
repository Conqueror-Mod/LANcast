/*
 * The settings index: every entry leads somewhere, and the search finds what
 * somebody would type.
 */
import { describe, it, expect } from "vitest";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { PANE_GROUPS, SETTINGS_INDEX, normalize, searchSettings } from "./settingsIndex";

const everyPane = new Set(PANE_GROUPS.flatMap((g) => g.panes.map((p) => p.id)));
const memberPanes = new Set(
  PANE_GROUPS.flatMap((g) => g.panes.filter((p) => !p.admin).map((p) => p.id)),
);

// The client's own source, as text, with the entities JSX writes for quotes
// and ampersands turned back into the characters they show.
function clientSource(): string {
  const root = join(__dirname, "..");
  const out: string[] = [];
  const walk = (dir: string) => {
    for (const e of readdirSync(dir, { withFileTypes: true })) {
      const p = join(dir, e.name);
      if (e.isDirectory()) walk(p);
      else if (e.name.endsWith(".tsx") && !e.name.includes(".test.")) out.push(readFileSync(p, "utf8"));
    }
  };
  walk(root);
  return normalize(
    out
      .join("\n")
      .replace(/&rsquo;|&#39;/g, "'")
      .replace(/&amp;/g, "&"),
  );
}

describe("the settings index", () => {
  it("names only panes that exist", () => {
    for (const e of SETTINGS_INDEX) expect(everyPane, e.label).toContain(e.pane);
  });

  /*
   * The drift check. A setting renamed on screen and not here leaves a result
   * that opens the right pane and then cannot find the row; this fails first.
   */
  it("uses labels that are still in the client", () => {
    const src = clientSource();
    const missing = SETTINGS_INDEX.filter((e) => !src.includes(normalize(e.label))).map((e) => e.label);
    expect(missing).toEqual([]);
  });

  it("has no entry twice", () => {
    const keys = SETTINGS_INDEX.map((e) => e.pane + ":" + normalize(e.label));
    expect(new Set(keys).size).toBe(keys.length);
  });
});

describe("searching settings", () => {
  it("finds a setting by the start of its words", () => {
    const got = searchSettings("sub sty", everyPane).map((m) => m.label);
    expect(got[0]).toBe("Subtitle style");
  });

  it("finds a setting by a word that is not in its name", () => {
    const got = searchSettings("replaygain", everyPane).map((m) => m.label);
    expect(got).toContain("Even out loudness");
  });

  it("puts a label that starts with the query first", () => {
    const got = searchSettings("night", everyPane).map((m) => m.label);
    expect(got[0]).toBe("Night mode");
  });

  it("offers panes as results too", () => {
    const got = searchSettings("dlna", everyPane);
    expect(got.some((m) => m.label === "DLNA" && m.pane === "dlna")).toBe(true);
  });

  it("names where each result lives", () => {
    const [m] = searchSettings("scrub bar", everyPane);
    expect(m).toMatchObject({ pane: "progress", paneLabel: "Playback & progress", group: "Server" });
  });

  // A member must not be offered the administrator's settings: the pane would
  // not open for them, so the result would be a lie.
  it("offers only panes this person can open", () => {
    const got = searchSettings("encoder", memberPanes);
    expect(got).toEqual([]);
    expect(searchSettings("passkeys", memberPanes).map((m) => m.label)).toContain("Passkeys");
  });

  it("matches curly and straight apostrophes alike", () => {
    expect(searchSettings("film’s frame", everyPane).map((m) => m.label)).toContain(
      "Match the display to the film's frame rate",
    );
  });

  it("answers nothing for an empty query", () => {
    expect(searchSettings("   ", everyPane)).toEqual([]);
  });
});
