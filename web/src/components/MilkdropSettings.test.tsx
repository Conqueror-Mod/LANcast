/*
 * The MilkDrop presets pane (ADR 0091, step d): every refusal in words a
 * person can act on, and the server's own reason passed through with the
 * file's name in front of it.
 */
import { describe, it, expect } from "vitest";
import { ApiFailure } from "@/api/client";
import { checkFile, uploadError, MILKDROP_MAX_BYTES } from "./MilkdropSettings";

describe("refusing a file before it is sent", () => {
  it("lets a .milk and a .json through", () => {
    expect(checkFile("Aurora.milk", 2000)).toBeNull();
    expect(checkFile("aurora.JSON", 2000)).toBeNull();
  });

  it("names the file and what to choose instead", () => {
    expect(checkFile("song.mp3", 2000)).toMatch(/“song\.mp3” is not a MilkDrop preset.*\.milk/);
  });

  it("refuses one over the server's limit, and an empty one", () => {
    expect(checkFile("big.milk", MILKDROP_MAX_BYTES + 1)).toMatch(/larger than 512 KB/);
    expect(checkFile("blank.milk", 0)).toMatch(/empty/);
  });
});

describe("saying why the server refused", () => {
  it("passes the parser's place and reason through, as a sentence", () => {
    const err = new ApiFailure(400, "invalid_preset", "per_frame_12, column 7: unknown function invsqrt()");
    expect(uploadError("Bad.milk", err)).toBe(
      "“Bad.milk” could not be used. per_frame_12, column 7: unknown function invsqrt().",
    );
  });

  it("says plainly when it is too large", () => {
    expect(uploadError("x.milk", new ApiFailure(413, "too_large", "body too large"))).toMatch(/larger than 512 KB/);
  });

  it("says who may upload, without blaming the file", () => {
    expect(uploadError("x.milk", new ApiFailure(403, "forbidden", "admin only"))).toBe(
      "Only an administrator can add or remove presets.",
    );
  });

  it("tells a network failure apart from a refusal", () => {
    expect(uploadError("x.milk", new TypeError("Failed to fetch"))).toMatch(/server could not be reached/);
  });
});
