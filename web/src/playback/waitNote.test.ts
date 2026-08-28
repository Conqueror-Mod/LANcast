import { describe, expect, it } from "vitest";

import { waitNote } from "./PlaybackProvider";

/*
 * What the player says while a file is being prepared.
 *
 * It used to append the server's own reason verbatim, which is a sentence
 * written for a log: *"Repackaging — matroska container is not supported, but
 * both codecs are"*. Every word true, and it reads as a complaint about the
 * file — a viewer asked why their MKV was unsupported when nothing was wrong.
 * The server was doing the cheapest thing it can do.
 */
describe("the waiting note", () => {
  it("does not tell a viewer their container is unsupported", () => {
    const note = waitNote({
      method: "remux",
      reason: "matroska container is not supported, but both codecs are",
    });
    expect(note).not.toContain("not supported");
    expect(note).not.toContain("matroska");
  });

  it("says a repackage is quick and lossless, because that is the difference", () => {
    // "My file is unsupported" and "it is being put in a different box" are
    // the two readings, and only one of them is true.
    const note = waitNote({ method: "remux" });
    expect(note).toContain("Repackaging");
    expect(note.toLowerCase()).toContain("nothing is re-encoded");
  });

  it("warns that a conversion takes longer, because it does", () => {
    const note = waitNote({ method: "transcode" });
    expect(note).toContain("Converting");
    expect(note).toMatch(/few seconds/);
  });

  // The two paths cost wildly different things — a container rewrite is a few
  // percent of a core, a conversion is most of one — so they must not read the
  // same. Reporting a copy as a transcode is a mistake this project has already
  // paid an hour for, in the activity panel.
  it("keeps the two paths distinguishable", () => {
    expect(waitNote({ method: "remux" })).not.toBe(
      waitNote({ method: "transcode" }),
    );
  });
});
