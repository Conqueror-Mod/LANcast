/*
 * The rules both sound surfaces share (the settings panel and the control
 * bar), tested once so the two cannot drift.
 */
import { describe, it, expect } from "vitest";
import { canBoostDialogue, nextDialogueLevel } from "./soundControls";

const t = (index: number, channels: number, isDefault = false) => ({
  index,
  channels,
  default: isDefault,
});

describe("canBoostDialogue", () => {
  it("offers it on stereo and surround, not on mono", () => {
    expect(canBoostDialogue([t(1, 6, true)], null)).toBe(true);
    expect(canBoostDialogue([t(1, 2, true)], null)).toBe(true);
    expect(canBoostDialogue([t(1, 1, true)], null)).toBe(false);
  });

  it("follows the chosen track, not the first one", () => {
    // A 5.1 main track and a mono commentary: choosing the commentary takes
    // the control away, and choosing the main track brings it back.
    const tracks = [t(1, 6, true), t(2, 1)];
    expect(canBoostDialogue(tracks, 2)).toBe(false);
    expect(canBoostDialogue(tracks, 1)).toBe(true);
  });

  it("uses the file's default track when none is chosen, even if it is not first", () => {
    expect(canBoostDialogue([t(1, 1), t(2, 6, true)], null)).toBe(true);
  });

  it("offers it when the channel count is unknown", () => {
    // Unknown is not mono. The client decides from what mpv decodes.
    expect(canBoostDialogue([{ index: 1, default: true } as never], null)).toBe(true);
    expect(canBoostDialogue([], null)).toBe(true);
  });
});

describe("nextDialogueLevel", () => {
  it("steps Off, Low, High, then back to Off", () => {
    expect(nextDialogueLevel(0)).toBe(1);
    expect(nextDialogueLevel(1)).toBe(2);
    expect(nextDialogueLevel(2)).toBe(0);
  });

  it("starts from Off when the stored level is nonsense", () => {
    // localStorage is a person's browser; a value from an old build or a
    // hand edit must not leave the button stuck.
    for (const bad of [-1, 3, 1.5, NaN, 99]) expect(nextDialogueLevel(bad)).toBe(1);
  });
});
