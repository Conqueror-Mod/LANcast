/*
 * The page's notes to the window's log (docs/logging-plan.md, Phase 2).
 *
 * The binding's own rules (areas, capping, cleaning, rate limits) are Go and
 * tested in cmd/lancast/clientnote_test.go. What is tested here is that the
 * page sends the right notes at the right moments, and that a note can never
 * break the app: no binding, a binding that throws, a binding that rejects.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { clientNote } from "./clientNote";
import { installErrorNotes, noteSignedOut } from "./errorNotes";
import { deny, clearDenials } from "@/playback/capabilities";

let notes: [string, string, string][];

beforeEach(() => {
  notes = [];
  window.lancastClientNote = vi.fn(async (level: string, area: string, message: string) => {
    notes.push([level, area, message]);
    return true;
  });
  localStorage.clear();
  clearDenials();
});

afterEach(() => {
  delete window.lancastClientNote;
  localStorage.clear();
});

describe("clientNote", () => {
  it("sends level, area and message to the window", () => {
    clientNote("warn", "playback", "x");
    expect(notes).toEqual([["warn", "playback", "x"]]);
  });

  it("does nothing in a browser tab, where there is no binding", () => {
    delete window.lancastClientNote;
    expect(() => clientNote("error", "error", "x")).not.toThrow();
  });

  it("never breaks the app when the binding throws or rejects", async () => {
    window.lancastClientNote = vi.fn(() => {
      throw new Error("binding gone");
    });
    expect(() => clientNote("error", "error", "x")).not.toThrow();

    const rejected = vi.fn(() => Promise.reject(new Error("refused")));
    window.lancastClientNote = rejected;
    expect(() => clientNote("error", "error", "x")).not.toThrow();
    // Let the rejection settle; an unhandled one would fail the run.
    await Promise.resolve();
    expect(rejected).toHaveBeenCalled();
  });
});

describe("what the page writes", () => {
  it("notes a claim withdrawn, once, not every time it is withheld", () => {
    expect(deny("hevc")).toBe(true);
    expect(deny("hevc")).toBe(false);
    const caps = notes.filter(([, area]) => area === "capabilities");
    expect(caps).toHaveLength(1);
    expect(caps[0][0]).toBe("warn");
    expect(caps[0][2]).toMatch(/stopped claiming hevc/);
  });

  it("notes being signed out, naming what asked", () => {
    noteSignedOut("libraries");
    expect(notes).toEqual([
      ["warn", "auth", expect.stringMatching(/not signed in \(401 on libraries\)/)],
    ]);
  });

  it("notes errors nothing caught, and rejected promises", () => {
    const target = new EventTarget();
    installErrorNotes(target as unknown as Window);

    target.dispatchEvent(
      Object.assign(new Event("error"), {
        message: "x is undefined",
        filename: "index.js",
        lineno: 12,
        colno: 3,
      }),
    );
    target.dispatchEvent(
      Object.assign(new Event("unhandledrejection"), { reason: new Error("fetch exploded") }),
    );

    const errs = notes.filter(([, area]) => area === "error");
    expect(errs).toHaveLength(2);
    expect(errs[0][2]).toBe("uncaught: x is undefined at index.js:12:3");
    expect(errs[1][2]).toMatch(/^unhandled rejection: fetch exploded/);
  });
});
