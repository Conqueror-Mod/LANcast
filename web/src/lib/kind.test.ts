/*
 * childCountLabel: the singular.
 *
 * The detail page rendered the plural label unconditionally and said "1 tracks"
 * under every single-track album — a small thing that makes a screen read as
 * generated rather than written, and the kind of thing nobody files a bug about
 * and everybody notices.
 */
import { describe, it, expect } from "vitest";
import { childCountLabel } from "./kind";

describe("childCountLabel", () => {
  it("says one track, not one tracks", () => {
    expect(childCountLabel(1, "track")).toBe("1 track");
    expect(childCountLabel(12, "track")).toBe("12 tracks");
  });

  it("singularises every label it knows", () => {
    expect(childCountLabel(1, "season")).toBe("1 season");
    expect(childCountLabel(1, "episode")).toBe("1 episode");
    expect(childCountLabel(1, "movie")).toBe("1 film");
    expect(childCountLabel(1, "photo")).toBe("1 photo");
  });

  // "Contents" is the fallback for an unfamiliar kind and has no singular worth
  // printing — "1 content" is worse than the plural it replaces.
  it("falls back to item for the generic label", () => {
    expect(childCountLabel(1, "wormhole")).toBe("1 item");
    expect(childCountLabel(3, "wormhole")).toBe("3 contents");
  });

  it("says zero in the plural", () => {
    expect(childCountLabel(0, "track")).toBe("0 tracks");
  });
});
