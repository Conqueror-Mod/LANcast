import { describe, it, expect } from "vitest";
import { skipTarget } from "./skip";
import type { components } from "@/api/schema";

type Marker = components["schemas"]["Marker"];

const intro = (startS: number, endS: number): Marker => ({
  kind: "intro",
  start_ms: startS * 1000,
  end_ms: endS * 1000,
  source: "fingerprint",
  confidence: 0.9,
  created_at: 0,
});

// Credits run to the end of the file, so they carry no end. That is real data,
// not a malformed row.
const credits = (startS: number): Marker => ({
  kind: "credits",
  start_ms: startS * 1000,
  source: "blackdetect",
  confidence: 0.9,
  created_at: 0,
});

describe("offering a skip", () => {
  it("offers nothing before the intro starts", () => {
    expect(skipTarget([intro(87, 117)], 40)).toBeNull();
  });

  it("offers the skip while the intro is playing", () => {
    expect(skipTarget([intro(87, 117)], 90)).toEqual({
      kind: "intro",
      atSeconds: 117,
    });
  });

  it("offers nothing once the intro has finished", () => {
    expect(skipTarget([intro(87, 117)], 118)).toBeNull();
  });

  /*
   * The boundaries, stated because an off-by-one here is a button that flickers
   * or one that offers to seek to where the playhead already is.
   */
  it("offers at the first instant and not at the last", () => {
    expect(skipTarget([intro(87, 117)], 87)).not.toBeNull();
    expect(skipTarget([intro(87, 117)], 117)).toBeNull();
  });

  /*
   * Credits are detected and deliberately not offered.
   *
   * Validated against forty films by looking at a frame thirty seconds past
   * each marker: about one in five was still in the film, mid-scene, with ten
   * minutes to run. A button that drops somebody out of the third act one time
   * in five is worse than no button — which is what ADR 0054 gated against.
   */
  it("never offers to skip credits", () => {
    expect(skipTarget([credits(5634)], 5700)).toBeNull();
    expect(skipTarget([credits(5634), intro(87, 117)], 5700)).toBeNull();
  });

  /*
   * An intro with no end has nowhere to skip to. The field is nullable because
   * credits genuinely have no end, so this is a real shape rather than a
   * malformed one.
   */
  it("ignores an intro that does not say where it ends", () => {
    const open = { ...intro(87, 117), end_ms: undefined } as Marker;
    expect(skipTarget([open], 90)).toBeNull();
  });

  it("ignores a range that ends before it begins", () => {
    expect(skipTarget([intro(117, 87)], 100)).toBeNull();
  });

  it("copes with no markers at all", () => {
    expect(skipTarget(undefined, 90)).toBeNull();
    expect(skipTarget([], 90)).toBeNull();
  });

  // A live stream reports NaN before metadata arrives, and a comparison against
  // NaN is false in both directions — which would otherwise be a silent no.
  it("copes with a position that is not a number yet", () => {
    expect(skipTarget([intro(87, 117)], NaN)).toBeNull();
  });

  // A recap before the titles, or a pilot with an extra sequence: whichever
  // range the playhead is in is the one to offer.
  it("picks the range the playhead is actually inside", () => {
    const markers = [intro(10, 40), intro(87, 117)];
    expect(skipTarget(markers, 95)?.atSeconds).toBe(117);
    expect(skipTarget(markers, 20)?.atSeconds).toBe(40);
    expect(skipTarget(markers, 60)).toBeNull();
  });

  /*
   * The end is taken exactly as detected.
   *
   * It is occasionally a few seconds early on a long title sequence, and
   * padding to compensate would risk clipping the first line of the episode —
   * the worse of the two, since nobody minds the tail of a theme.
   */
  it("seeks to the detected end, with nothing added", () => {
    expect(skipTarget([intro(200, 245)], 210)?.atSeconds).toBe(245);
  });
});
