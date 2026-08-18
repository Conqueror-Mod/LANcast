/*
 * The subtitle button appears only when it can do something.
 *
 * It was gated on "this is not audio" alone, so every video without subtitles
 * carried a CC button whose click was a silent no-op: `cycleSub` cycles
 * `[null, ...available]`, and with nothing available that list is one element
 * long and the cycle lands back on off. A control that never responds is
 * indistinguishable from a broken one, and it got reported as leftover UI.
 */
import { describe, it, expect } from "vitest";
import { showsSubtitleButton } from "./subtitleButton";
import type { SubtitleTrack } from "@/api/types";

function track(over: Partial<SubtitleTrack> = {}): SubtitleTrack {
  return {
    key: "0",
    label: "English",
    source: "embedded",
    forced: false,
    default: false,
    available: true,
    ...over,
  };
}

describe("the player's subtitle button", () => {
  it("shows when there is a track to cycle to", () => {
    expect(showsSubtitleButton(false, [track()])).toBe(true);
  });

  // The case this exists for: a video with no subtitles at all.
  it("stays hidden when the file has no subtitles", () => {
    expect(showsSubtitleButton(false, [])).toBe(false);
  });

  /*
   * The case that a track *count* would get wrong. A bitmap track is real and
   * appears in the menu with its reason, but cycleSub skips it — so counting
   * tracks here would restore the dead button on exactly the files that make
   * it look most broken.
   */
  it("stays hidden when every track is unusable", () => {
    expect(
      showsSubtitleButton(false, [
        track({ available: false, reason: "bitmap subtitles cannot be converted" }),
      ]),
    ).toBe(false);
  });

  it("shows when a usable track sits alongside an unusable one", () => {
    expect(
      showsSubtitleButton(false, [
        track({ key: "0", available: false }),
        track({ key: "1", available: true }),
      ]),
    ).toBe(true);
  });

  // A song has no subtitles to offer, whatever the list says.
  it("never shows for audio", () => {
    expect(showsSubtitleButton(true, [track()])).toBe(false);
  });
});
