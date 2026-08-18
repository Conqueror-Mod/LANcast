/*
 * Which libraries can be put on as a queue.
 *
 * The rule is small and the mistakes it prevents are not: queueing a show
 * library's *shows* hands the player containers it cannot advance through, and
 * offering "play all" on a picture library promises a slideshow that does not
 * exist.
 */
import { describe, it, expect } from "vitest";
import { playableKindFor } from "./hooks";

describe("what a library plays", () => {
  it("queues tracks for music", () => {
    expect(playableKindFor("music")).toBe("track");
  });

  // The one worth pinning: episodes, not shows.
  it("queues episodes for a show library, not shows", () => {
    expect(playableKindFor("show")).toBe("episode");
  });

  it("queues films for a movie library", () => {
    expect(playableKindFor("movie")).toBe("movie");
  });

  /*
   * Pictures get nothing until a slideshow exists. A queue of photographs would
   * advance at the pace of a player built for films, which is not a slideshow —
   * it is a bug that looks like a feature.
   */
  it("offers nothing for pictures", () => {
    expect(playableKindFor("picture")).toBeNull();
  });

  it("offers nothing for an other library", () => {
    expect(playableKindFor("other")).toBeNull();
  });

  // An unknown kind falls back to films, matching configForKind, so a library
  // kind added server-side does not lose the button until the client catches up.
  it("falls back to films for an unknown kind", () => {
    expect(playableKindFor("hologram")).toBe("movie");
    expect(playableKindFor(undefined)).toBe("movie");
  });
});
