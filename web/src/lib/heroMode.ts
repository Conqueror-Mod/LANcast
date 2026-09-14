import { useDevice } from "./device";
import type { Item } from "@/api/types";
import { isMusic, isPicture } from "./kind";

/*
 * What the homepage spotlight shows.
 *
 * The hero was a fixed rule: the first resumable item carrying fanart, else the
 * first recently added. Resume-wins is right for the common case and wrong for
 * the one people actually complain about — a library nobody is mid-way through,
 * where the hero becomes a permanent monument to the last thing anybody
 * watched. A film abandoned in March is still the first thing the house sees in
 * September.
 *
 * Per device, beside `bigscreen`, `tileSize` and `spoilers`, and for the same
 * reason: the hero is what one person sees on one screen, there is no per-user
 * preference store on the server, and inventing one for a spotlight would be a
 * schema decision made by a radio button.
 */

export const HERO_MODE_KEY = "lancast:hero-mode";
export const HERO_PINNED_KEY = "lancast:hero-pinned";

export type HeroMode = "continue" | "recent" | "recommended" | "pinned";

export const HERO_MODE_DEFAULT: HeroMode = "continue";

export function useHeroMode(): [HeroMode, (m: HeroMode) => void] {
  return useDevice<HeroMode>(HERO_MODE_KEY, HERO_MODE_DEFAULT);
}

/**
 * The item pinned by hand, or 0 for none.
 *
 * Stored separately from the mode so that switching to Recently added and back
 * does not forget the choice — and so "pinned" can fall back gracefully when
 * the pinned item has since been removed from the library.
 */
export function usePinnedHero(): [number, (id: number) => void] {
  return useDevice<number>(HERO_PINNED_KEY, 0);
}

/*
 * A hero needs a backdrop to be a hero at all.
 *
 * Music and pictures are excluded rather than left to fail the fanart test.
 * Neither has a backdrop today and both would be skipped anyway, but "the hero
 * is for something you watch" is the actual rule, and leaving it implicit means
 * the first album that arrives with provider artwork — or the first photo wide
 * enough to look like one — silently becomes a hero.
 */
export function heroEligible(item: Item | undefined | null): boolean {
  if (!item) return false;
  return !!item.artwork?.fanart && !item.missing && !isMusic(item) && !isPicture(item);
}

function firstEligible(items: Item[] | undefined): Item | undefined {
  return items?.find(heroEligible);
}

/** Why this item is in the spotlight, which is what the hero labels itself. */
export type HeroReason = "resuming" | "recent" | "recommended" | "pinned";

export interface HeroPick {
  item: Item;
  reason: HeroReason;
  /**
   * For "recommended", the item this was suggested from. The hero says
   * "Because you watched X" and that claim has to be attributable — a
   * recommendation that cannot say where it came from is the thing this project
   * does not want to grow.
   */
  seed?: Item;
}

export interface HeroSources {
  resumable?: Item[];
  recent?: Item[];
  /** Unwatched candidates sharing a genre with `seed`. */
  recommended?: Item[];
  seed?: Item;
  pinned?: Item;
}

/*
 * pickHero chooses the spotlight.
 *
 * The fallback chain is the load-bearing part. A mode with no qualifying
 * candidate falls through to the next rather than rendering an empty slab: a
 * new library has nothing to resume, a fully-watched one has nothing to
 * recommend, and a pinned item can be deleted by somebody else. Every one of
 * those is ordinary, and none of them is a reason to show a grey rectangle
 * where the biggest thing on the page should be.
 *
 * Continue is the last resort as well as the first choice, because it is the
 * one list that is *about* the person looking at it.
 */
export function pickHero(mode: HeroMode, sources: HeroSources): HeroPick | null {
  const resuming = firstEligible(sources.resumable);
  const fresh = firstEligible(sources.recent);

  const fromPin = (): HeroPick | null =>
    heroEligible(sources.pinned)
      ? { item: sources.pinned as Item, reason: "pinned" }
      : null;

  const fromRecommendation = (): HeroPick | null => {
    const found = firstEligible(sources.recommended);
    // Without a seed there is no sentence to write, so it is not a
    // recommendation — it is an unwatched item, which the next mode along
    // already covers honestly.
    if (!found || !sources.seed) return null;
    return { item: found, reason: "recommended", seed: sources.seed };
  };

  const order: Array<() => HeroPick | null> = [];
  switch (mode) {
    case "pinned":
      order.push(fromPin);
      break;
    case "recommended":
      order.push(fromRecommendation);
      break;
    case "recent":
      order.push(() => (fresh ? { item: fresh, reason: "recent" } : null));
      break;
    case "continue":
    default:
      break;
  }
  // The chain every mode ends with.
  order.push(() => (resuming ? { item: resuming, reason: "resuming" } : null));
  order.push(() => (fresh ? { item: fresh, reason: "recent" } : null));

  for (const step of order) {
    const pick = step();
    if (pick) return pick;
  }
  return null;
}

/*
 * The genres a recommendation is drawn from.
 *
 * Capped, because the query is one repeated parameter per genre and a film
 * carrying eight of them stops being a recommendation and becomes "anything
 * unwatched". Three is enough to mean something and few enough to still narrow.
 */
export const HERO_GENRE_LIMIT = 3;

export function seedGenres(seed: Item | undefined): string[] {
  return (seed?.genres ?? []).slice(0, HERO_GENRE_LIMIT);
}
