/*
 * What the homepage spotlight shows.
 *
 * The assertions that matter are the fallbacks. A mode with no qualifying
 * candidate has to fall through rather than render an empty slab, and every one
 * of the ways that happens is ordinary: a new library has nothing to resume, a
 * fully-watched one has nothing to recommend, and a pinned item can be deleted
 * by somebody else while you are looking at the page.
 */
import { describe, it, expect } from "vitest";
import { pickHero, heroEligible, seedGenres, HERO_GENRE_LIMIT } from "./heroMode";
import type { Item } from "@/api/types";

function item(over: Partial<Item> & { id: number }): Item {
  return {
    kind: "movie",
    title: `Item ${over.id}`,
    library_id: 1,
    year: null,
    series: null,
    season: null,
    episode: null,
    container: null,
    size_bytes: null,
    duration_ms: null,
    added_at: 0,
    missing: false,
    parent_id: null,
    overview: null,
    rating: null,
    content_rating: null,
    released_at: null,
    provider: null,
    external_id: null,
    match_state: "matched",
    match_score: null,
    metadata_updated_at: null,
    probed_at: null,
    video_codec: null,
    video_profile: null,
    width: null,
    height: null,
    video_bitrate: null,
    frame_rate: null,
    audio_codec: null,
    audio_channels: null,
    artwork: { fanart: "/art/fanart.jpg" },
    ...over,
  } as Item;
}

const resumable = [item({ id: 1, title: "Half-watched" })];
const recent = [item({ id: 2, title: "Just arrived" })];
const suggestion = [item({ id: 3, title: "Never watched" })];
const seed = item({ id: 1, title: "Half-watched", genres: ["Horror", "Thriller"] });

describe("what counts as a hero at all", () => {
  it("refuses an item with no backdrop", () => {
    // A hero with no fanart is a grey slab. It is not a smaller hero.
    expect(heroEligible(item({ id: 9, artwork: {} }))).toBe(false);
  });

  it("refuses music and pictures even when they have art", () => {
    /*
     * "The hero is for something you watch" is the actual rule. Left implicit,
     * the first album to arrive with provider artwork silently becomes a hero.
     */
    expect(heroEligible(item({ id: 9, kind: "album" }))).toBe(false);
    expect(heroEligible(item({ id: 9, kind: "photo" }))).toBe(false);
  });

  it("refuses an item whose file has gone", () => {
    expect(heroEligible(item({ id: 9, missing: true }))).toBe(false);
  });
});

describe("the mode decides what is tried first", () => {
  it("resumes by default, which is what people open LANcast for", () => {
    const pick = pickHero("continue", { resumable, recent });
    expect(pick?.item.id).toBe(1);
    expect(pick?.reason).toBe("resuming");
  });

  it("shows the newest arrival when asked to", () => {
    /*
     * The complaint this setting exists for: a library nobody is mid-way
     * through, where the hero is a permanent monument to the last thing
     * anybody watched.
     */
    const pick = pickHero("recent", { resumable, recent });
    expect(pick?.item.id).toBe(2);
    expect(pick?.reason).toBe("recent");
  });

  it("shows the pinned item ahead of everything else", () => {
    const pinned = item({ id: 7, title: "Chosen by hand" });
    const pick = pickHero("pinned", { resumable, recent, pinned });
    expect(pick?.item.id).toBe(7);
    expect(pick?.reason).toBe("pinned");
  });

  it("names what a recommendation came from", () => {
    // A recommendation that cannot say where it came from is the thing this
    // project does not want to grow.
    const pick = pickHero("recommended", { resumable, recent, recommended: suggestion, seed });
    expect(pick?.item.id).toBe(3);
    expect(pick?.seed?.title).toBe("Half-watched");
  });
});

describe("a mode with nothing to show falls through", () => {
  it("falls back when the pinned item has gone", () => {
    // Somebody else removed it from the library. That is not an error state
    // and it is certainly not an empty spotlight.
    const pick = pickHero("pinned", { resumable, recent, pinned: undefined });
    expect(pick?.item.id).toBe(1);
    expect(pick?.reason).toBe("resuming");
  });

  it("falls back when there is nothing left to recommend", () => {
    const pick = pickHero("recommended", { resumable, recent, recommended: [], seed });
    expect(pick?.reason).toBe("resuming");
  });

  it("is not a recommendation without something to attribute it to", () => {
    // Candidates but no seed is an unwatched item, not a suggestion, and
    // calling it one would put an unearned sentence on the page.
    const pick = pickHero("recommended", { recent, recommended: suggestion });
    expect(pick?.reason).toBe("recent");
  });

  it("reaches recently added when there is nothing to resume", () => {
    const pick = pickHero("continue", { resumable: [], recent });
    expect(pick?.item.id).toBe(2);
  });

  it("skips candidates with no backdrop rather than showing one", () => {
    const artless = [item({ id: 4, artwork: {} }), item({ id: 5 })];
    expect(pickHero("recent", { recent: artless })?.item.id).toBe(5);
  });

  it("shows nothing at all when a fresh install has nothing", () => {
    // Null, so the page renders no spotlight rather than a hero-shaped hole.
    expect(pickHero("continue", {})).toBeNull();
  });
});

describe("the genres a recommendation is drawn from", () => {
  it("caps them, or the suggestion becomes 'anything unwatched'", () => {
    const many = item({ id: 8, genres: ["A", "B", "C", "D", "E"] });
    expect(seedGenres(many)).toHaveLength(HERO_GENRE_LIMIT);
  });

  it("is empty for a seed nothing is known about", () => {
    expect(seedGenres(item({ id: 8 }))).toEqual([]);
    expect(seedGenres(undefined)).toEqual([]);
  });
});
