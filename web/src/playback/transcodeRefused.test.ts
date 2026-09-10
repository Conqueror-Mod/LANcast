import { describe, it, expect } from "vitest";
import { refusedNote } from "./transcodeRefused";

/*
 * What the player says when the server will not start playback.
 *
 * The old text named a cause it could not know: "the server may already be
 * converting as much as it can". That is one refusal among several, and it was
 * stated as though it were the finding — so every failure on this path pointed
 * at load.
 *
 * It cost a real investigation. A show whose path is a directory was handed to
 * the player, the server refused it at the containment check, and this text
 * sent everybody to look at how busy the server was. The server was idle.
 *
 * A `<video>` element reports MEDIA_ERR codes and nothing else, and re-asking
 * would start the very transcode that was refused — so the honest position is
 * that the request was refused, that waiting will not help, and where the
 * answer is. These hold that honesty in place.
 */

describe("what the player says when playback is refused", () => {
  it("does not claim the server is busy when it does not know", () => {
    const note = refusedNote({ missing: false });

    expect(
      note,
      "naming one cause as the explanation is what sent a directory-path " +
        "refusal to the Activity panel for an hour",
    ).not.toMatch(/already be converting as much as it can/i);
  });

  // Both real possibilities, neither asserted, and somewhere to actually look.
  it("offers the possibilities and says where the answer is", () => {
    const note = refusedNote({ missing: false });

    expect(note).toMatch(/busy converting/i);
    expect(note).toMatch(/unable to read the file/i);
    expect(note).toMatch(/log/i);
  });

  /*
   * Except where the item already says so.
   *
   * `missing` is on the payload in hand — no request, no transcode, no guess —
   * so a file the server has marked absent is a certainty rather than one of
   * several possibilities, and gets told as one.
   */
  it("says plainly when the file is known to be gone", () => {
    const note = refusedNote({ missing: true });

    expect(note).toMatch(/not on the server/i);
    expect(note).not.toMatch(/busy converting/i);
  });

  // A drive that is unplugged is the ordinary reason, and it is recoverable —
  // scanning marks missing rather than deleting, so the library is intact.
  it("points a missing file at the likely reason and the way back", () => {
    const note = refusedNote({ missing: true });

    expect(note).toMatch(/disconnected/i);
    expect(note).toMatch(/scan/i);
  });

  // Nothing here ever tells somebody to sit and wait: every refusal on this
  // path is a decision the server already made, and none of them expire.
  it("never suggests that waiting will fix it", () => {
    for (const missing of [true, false]) {
      expect(refusedNote({ missing })).not.toMatch(/try again shortly/i);
    }
  });
});
