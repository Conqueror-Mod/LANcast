import { describe, expect, it } from "vitest";
import { daysBetween, resumeArt, resumeDetail, resumeFraction, resumeHref, resumeTarget } from "./resume";
import type { Item, ResumeCard } from "@/api/types";

const item = (over: Partial<Item>): Item => ({ id: 1, kind: "movie", title: "T", ...over }) as Item;
const card = (over: Partial<ResumeCard>): ResumeCard =>
  ({ kind: "video", touched_at: 0, item: item({}), ...over }) as ResumeCard;

describe("resumeDetail says where you are in the kind's own terms", () => {
  it("a film has time left", () => {
    const c = card({ item: item({ duration_ms: 7_200_000, progress: { position_ms: 2_880_000, watched: false, watch_count: 0 } }) });
    expect(resumeDetail(c)).toBe("1h 12m left");
  });
  it("a collection's next film names the collection", () => {
    const c = card({
      item: item({ continues_from: { collection_id: 8, collection_title: "A Franchise", after_id: 7, after_title: "One" } }),
    });
    expect(resumeDetail(c)).toBe("Next in A Franchise");
  });
  it("a show names its next episode", () => {
    const ep = item({ kind: "episode", season: 2, episode: 4 });
    expect(resumeDetail(card({ kind: "show", item: item({ kind: "show", next_episode: ep }) }))).toBe("S02E04 · next");
  });
  it("an album says which track of how many", () => {
    const c = card({ kind: "music", item: item({ kind: "album" }), track: { item: item({ id: 9, kind: "track" }), number: 6, of: 11 } });
    expect(resumeDetail(c)).toBe("Track 6 of 11");
    expect(resumeFraction(c)).toBeCloseTo(6 / 11);
  });
  it("a comic counts pages from one, though the place is from zero", () => {
    const c = card({ kind: "comic", item: item({ kind: "comic" }), page: { page: 40, pages: 120, updated_at: 0 } });
    expect(resumeDetail(c)).toBe("Page 41 of 120");
  });
  it("a game says when it was saved, and draws no bar", () => {
    const now = new Date(2026, 10, 8, 0, 10).getTime();
    const lateLastNight = new Date(2026, 10, 7, 23, 50).getTime() / 1000;
    const c = card({ kind: "game", item: item({ kind: "rom" }), touched_at: lateLastNight });
    expect(resumeDetail(c, now)).toBe("Saved yesterday");
    expect(resumeFraction(c)).toBeNull();
  });
});

describe("daysBetween counts local calendar days", () => {
  it("twenty minutes across midnight is one day", () => {
    expect(daysBetween(new Date(2026, 10, 7, 23, 50).getTime(), new Date(2026, 10, 8, 0, 10).getTime())).toBe(1);
  });
  it("twenty hours on one date is none", () => {
    expect(daysBetween(new Date(2026, 10, 8, 1).getTime(), new Date(2026, 10, 8, 21).getTime())).toBe(0);
  });
});

describe("what a card draws, opens and writes to", () => {
  it("a show draws the next episode's still, wide", () => {
    const ep = item({ kind: "episode", artwork: { thumb: "still" } as Item["artwork"] });
    const art = resumeArt(card({ kind: "show", item: item({ kind: "show", next_episode: ep }) }));
    expect(art).toEqual({ src: "/api/artwork/still?size=poster", shape: "wide" });
  });
  it("a comic opens at your page; everything else opens its page", () => {
    expect(resumeHref(card({ kind: "comic", item: item({ id: 5, kind: "comic" }) }))).toBe("/read/5");
    expect(resumeHref(card({ kind: "book", item: item({ id: 6, kind: "book" }) }))).toBe("/item/6");
  });
  it("an album card's Mark and Remove write to the track, not the album", () => {
    const tr = item({ id: 9, kind: "track" });
    expect(resumeTarget(card({ kind: "music", item: item({ kind: "album" }), track: { item: tr, number: 1 } })).id).toBe(9);
  });
});
