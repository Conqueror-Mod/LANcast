import { describe, it, expect } from "vitest";
import { CREDITS_LEAD_SECONDS, GATED_CREDITS, skipTarget } from "./skip";
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
// not a malformed row. The default source is the ungated rule's, the one that
// was wrong one time in five.
const credits = (startS: number, source = "blackdetect"): Marker => ({
  kind: "credits",
  start_ms: startS * 1000,
  source,
  confidence: 0.9,
  created_at: 0,
});
const gated = (startS: number) => credits(startS, GATED_CREDITS);

// A 100-minute film whose gated marker is at 94%.
const FILM = 6000;

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
   * Markers from the ungated rule are never offered.
   *
   * Checked against forty films by looking at frames around each marker, about
   * one in five was still in the film, mid-scene, with ten minutes to run. A
   * library part-way through re-examination still holds them.
   */
  it("never offers credits from the ungated rule", () => {
    expect(skipTarget([credits(5640)], 5700, FILM)).toBeNull();
    expect(skipTarget([credits(5640), intro(87, 117)], 5700, FILM)).toBeNull();
  });

  /*
   * On an episode the ungated rule is trusted: early on one episode in
   * thirty-three, against one film in five.
   */
  it("offers credits from the ungated rule on an episode, not on a film", () => {
    expect(skipTarget([credits(5640)], 5700, FILM, "episode")?.kind).toBe("credits");
    expect(skipTarget([credits(5640)], 5700, FILM, "movie")).toBeNull();
    expect(skipTarget([credits(5640)], 5700, FILM)).toBeNull();
  });

  // A source nobody vouched for is not offered on either.
  it("offers credits from an unknown source on neither", () => {
    expect(skipTarget([credits(5640, "guess")], 5700, FILM, "episode")).toBeNull();
  });

  it("offers a gated credits skip from the marker to near the end", () => {
    expect(skipTarget([gated(5640)], 5639, FILM)).toBeNull();
    expect(skipTarget([gated(5640)], 5640, FILM)).toEqual({
      kind: "credits",
      atSeconds: FILM - CREDITS_LEAD_SECONDS,
    });
    expect(skipTarget([gated(5640)], 5900, FILM)?.kind).toBe("credits");
  });

  // Once there is nothing left to skip the button goes, rather than offering
  // to seek backwards to where it would land.
  it("stops offering once the playhead reaches where it would land", () => {
    const to = FILM - CREDITS_LEAD_SECONDS;
    expect(skipTarget([gated(5640)], to - 0.1, FILM)).not.toBeNull();
    expect(skipTarget([gated(5640)], to, FILM)).toBeNull();
  });

  /*
   * A marker outside the window is not believed. Alien 3's said 73.2%: chosen
   * against a length the file did not have, and an hour of film after it.
   */
  it("refuses a credits marker outside its own file's window", () => {
    expect(skipTarget([gated(4392)], 4400, FILM)).toBeNull(); // 73.2%
    expect(skipTarget([gated(5280)], 5300, FILM)).not.toBeNull(); // 88%
  });

  // In the last hundredth there is nothing worth a button.
  it("does not offer a skip from the last one percent", () => {
    expect(skipTarget([gated(5940)], 5950, FILM)).toBeNull(); // 99%
  });

  it("offers no credits skip without a known duration", () => {
    expect(skipTarget([gated(5640)], 5700)).toBeNull();
    expect(skipTarget([gated(5640)], 5700, NaN)).toBeNull();
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
