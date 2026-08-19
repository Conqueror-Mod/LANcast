/*
 * The spoiler rule.
 *
 * A synopsis is written as a summary, not a tease — "Leela discovers who her
 * parents are" is the plot — so the next row down a season list can give away
 * the thing you were about to watch. Doing nothing about that is a choice, and
 * the wrong one to make by accident (season-page-plan.md §5).
 *
 * The rule is worth testing rather than the rendering, because both ways of
 * getting it wrong are bad in different directions: a spoiler shown, or a season
 * page stripped of the information it exists to carry.
 */
import { describe, it, expect } from "vitest";
import { spoilerState, SPOILER_DEFAULT, type SpoilerMode } from "./spoilers";

const unstarted = {};
const started = { progress: { position_ms: 120_000, watched: false } };
const watched = { progress: { position_ms: 0, watched: true } };

describe("hiding spoilers", () => {
  it("hides the synopsis of an episode nobody has started, by default", () => {
    expect(spoilerState(SPOILER_DEFAULT, unstarted)).toEqual({
      hideSynopsis: true,
      hideStill: false,
    });
  });

  /*
   * The still stays at the default setting. A frame rarely gives a plot away,
   * and it is what makes a row identifiable at a glance — hiding it by default
   * would trade a real loss for a small gain.
   */
  it("keeps the still at the default setting", () => {
    expect(spoilerState("synopsis", unstarted).hideStill).toBe(false);
  });

  it("hides both at the strongest setting", () => {
    expect(spoilerState("all", unstarted)).toEqual({
      hideSynopsis: true,
      hideStill: true,
    });
  });

  it("hides nothing when asked to show everything", () => {
    expect(spoilerState("show", unstarted)).toEqual({
      hideSynopsis: false,
      hideStill: false,
    });
  });

  /*
   * The refinement that keeps the setting from being switched off in annoyance:
   * two minutes into an episode you have already met whatever the first scene
   * gives away, so protecting it from you is the guard getting in the way of the
   * person it is for.
   */
  it("shows everything for an episode already started", () => {
    for (const mode of ["synopsis", "all"] as SpoilerMode[]) {
      expect(spoilerState(mode, started)).toEqual({
        hideSynopsis: false,
        hideStill: false,
      });
    }
  });

  it("shows everything for an episode already watched", () => {
    expect(spoilerState("all", watched)).toEqual({
      hideSynopsis: false,
      hideStill: false,
    });
  });

  // Hiding the synopsis is the default, because the alternative is a season page
  // that spoils the next episode of everything anybody is part way through.
  it("defaults to hiding the synopsis", () => {
    expect(SPOILER_DEFAULT).toBe("synopsis");
  });
});
