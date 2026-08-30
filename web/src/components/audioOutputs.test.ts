import { describe, expect, it } from "vitest";

import { outputsWithheld, routableOutputs } from "./audioOutputs";

/*
 * Reported as "audio devices only display output 1".
 *
 * Measured in Chromium 148 with microphone permission denied: `enumerateDevices`
 * returns exactly one `audiooutput`, with an empty deviceId and an empty label.
 * The picker's numbered fallback turned that single placeholder into "Output 1",
 * which reads as one device found rather than none offered — and selecting it
 * does nothing, because an empty id already means "system default".
 */
describe("what the browser is really offering", () => {
  it("recognises the single empty placeholder as nothing at all", () => {
    expect(outputsWithheld([{ id: "", label: "" }])).toBe(true);
  });

  it("is not fooled by a device that merely has no name", () => {
    // A real device with a hidden label can still be routed to. The id is the
    // thing that matters, which is why the test is on the id.
    expect(outputsWithheld([{ id: "a1b2c3", label: "" }])).toBe(false);
  });

  it("says nothing is withheld when there is a real list", () => {
    expect(
      outputsWithheld([
        { id: "default", label: "Speakers" },
        { id: "a1b2", label: "HDMI" },
      ]),
    ).toBe(false);
  });

  it("treats an empty answer as no devices rather than as withholding", () => {
    // A machine with no outputs is a different statement from a browser
    // refusing to say, and the row says different things about them.
    expect(outputsWithheld([])).toBe(false);
  });

  it("drops the placeholder from what can be picked", () => {
    // "Auto select device" already means the default, so offering the empty-id
    // entry beside it is one choice wearing two names.
    expect(routableOutputs([{ id: "", label: "Output 1" }])).toEqual([]);
    expect(
      routableOutputs([
        { id: "", label: "Output 1" },
        { id: "hdmi", label: "HDMI" },
      ]),
    ).toEqual([{ id: "hdmi", label: "HDMI" }]);
  });
});
