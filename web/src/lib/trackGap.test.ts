import { describe, it, expect } from "vitest";
import { TrackGapMeter } from "./trackGap";

describe("the gap between two tracks", () => {
  it("splits the silence at the next source's loadstart", () => {
    const m = new TrackGapMeter();
    m.ended(1000);
    m.loadstart(1450);
    expect(m.playing(1700)).toEqual({ total: 700, toSource: 450, load: 250 });
  });

  it("measures nothing for a play that follows no track end", () => {
    const m = new TrackGapMeter();
    expect(m.playing(500)).toBeNull();
    m.ended(1000);
    m.playing(1200);
    expect(m.playing(5000)).toBeNull();
  });

  it("keeps the first loadstart after an end, and forgets an end on reset", () => {
    const m = new TrackGapMeter();
    m.ended(0);
    m.loadstart(100);
    m.loadstart(900);
    expect(m.playing(1000)?.toSource).toBe(100);
    m.ended(2000);
    m.reset();
    expect(m.playing(2500)).toBeNull();
  });
});
