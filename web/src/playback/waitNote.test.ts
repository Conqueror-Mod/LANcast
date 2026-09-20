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

/*
 * How long the wait actually is, which is a different question from what is
 * being done — and the two answers are the opposite way round from what anyone
 * expects.
 *
 * A film whose picture is *copied* and whose sound is re-encoded is the
 * slowest thing to start. The server can only serve a playlist it can describe
 * completely, and a copy's segment boundaries fall on the source's own
 * keyframes, which nobody knows in advance — so it waits for the whole
 * conversion. A full re-encode, where the server picks the boundaries, writes
 * its playlist up front and starts in about a second.
 *
 * Measured as the installed service on Jay and Silent Bob Reboot (H.264 + DTS,
 * 1h45, resuming at 6m40): one minute fifty-eight. The message this replaced
 * promised "a few seconds", and the consequence was not cosmetic — a working
 * conversion was reported as the app being broken, and backing out of it
 * superseded the session and destroyed 111 seconds of finished work.
 */
describe("how long the note says to expect", () => {
  it("warns that an audio-only conversion takes minutes, not seconds", () => {
    const note = waitNote({ method: "transcode", video_action: "copy" });

    expect(note.toLowerCase()).toContain("minute");
    expect(note).not.toMatch(/few seconds/);
    // It names which stream, so nobody reads it as their picture being degraded.
    expect(note).toContain("picture is untouched");
  });

  /*
   * And it says not to retry, which is not politeness: a second request for
   * the same item supersedes the first and throws away a conversion that was
   * nearly done.
   */
  it("says that starting again makes it slower", () => {
    const note = waitNote({ method: "transcode", video_action: "copy" });
    expect(note).toContain("Starting it again makes it slower");
  });

  it("does not warn about minutes when the picture is being re-encoded", () => {
    const note = waitNote({ method: "transcode", video_action: "encode" });

    expect(note.toLowerCase()).not.toContain("minute");
    expect(note).toMatch(/few seconds/);
  });

  // An older server, or a client-side fallback that has not been told which
  // action the server will choose, must still say something sane rather than
  // promising the wrong one of the two.
  it("falls back to the general message when the action is unknown", () => {
    const note = waitNote({ method: "transcode" });
    expect(note).toContain("Converting");
  });

  // All three must set different expectations, or the distinction is lost.
  it("says something different for each case", () => {
    const notes = new Set([
      waitNote({ method: "remux", video_action: "copy" }),
      waitNote({ method: "transcode", video_action: "copy" }),
      waitNote({ method: "transcode", video_action: "encode" }),
    ]);
    expect(notes.size).toBe(3);
  });
});
