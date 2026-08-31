import { describe, it, expect, beforeEach } from "vitest";
import {
  acknowledge,
  forgetAcknowledgements,
  isRevealed,
  revealed,
} from "./sensitiveAck";
import type { Item } from "@/api/types";

/*
 * What may be drawn (ADR 0051).
 *
 * The rule is four lines and it decides whether a photograph appears on
 * somebody's screen, so it is tested as a rule rather than through a component.
 */
function item(over: Partial<Item>): Item {
  return {
    id: 1,
    library_id: 1,
    kind: "photo",
    title: "a",
    parent_id: null,
    artwork: {},
    ...over,
  } as Item;
}

beforeEach(() => forgetAcknowledgements());

describe("revealed", () => {
  // Everything not marked is drawn, which is every item in every library
  // belonging to everyone who never turns this on.
  it("draws an unmarked item without being asked", () => {
    expect(revealed(item({ sensitive: false }), new Set())).toBe(true);
    expect(revealed(item({}), new Set())).toBe(true);
  });

  it("covers a marked item until it is acknowledged", () => {
    const photo = item({ id: 7, sensitive: true });
    expect(revealed(photo, new Set())).toBe(false);
    expect(revealed(photo, new Set([7]))).toBe(true);
  });

  /*
   * The cascade, and the reason it exists.
   *
   * Entering a marked folder is acknowledging it. Being asked again by each of
   * the two hundred photographs inside is not a privacy feature — it is the
   * reason somebody turns the setting off.
   */
  it("draws a photo inside a folder that has been acknowledged", () => {
    const photo = item({ id: 7, sensitive: true, parent_id: 3 });
    expect(revealed(photo, new Set([3]))).toBe(true);
  });

  // But acknowledging one folder is not acknowledging the library. A photo
  // whose parent is a *different* folder stays covered.
  it("does not draw a photo from a folder nobody acknowledged", () => {
    const photo = item({ id: 7, sensitive: true, parent_id: 4 });
    expect(revealed(photo, new Set([3]))).toBe(false);
  });

  // A loose photo with no parent cannot be revealed by acknowledging something
  // else — the null parent must not match anything.
  it("never lets a null parent match an acknowledgement", () => {
    const photo = item({ id: 7, sensitive: true, parent_id: null });
    expect(revealed(photo, new Set([0]))).toBe(false);
  });
});

describe("the acknowledgement store", () => {
  it("remembers what was acknowledged", () => {
    const photo = item({ id: 7, sensitive: true });
    expect(isRevealed(photo)).toBe(false);
    acknowledge(7);
    expect(isRevealed(photo)).toBe(true);
  });

  // And the cascade holds against the live store, not only against a set a
  // test built by hand.
  it("reveals a photo whose folder was acknowledged", () => {
    const photo = item({ id: 7, sensitive: true, parent_id: 3 });
    acknowledge(3);
    expect(isRevealed(photo)).toBe(true);
  });

  /*
   * And forgets on demand, which is what signing out has to do.
   *
   * The acknowledgement is per person, and a session store that survives one
   * person leaving and another arriving is a session store that shows the
   * second person what the first agreed to look at.
   */
  it("forgets everything when asked", () => {
    const photo = item({ id: 7, sensitive: true });
    acknowledge(7);
    forgetAcknowledgements();
    expect(isRevealed(photo)).toBe(false);
  });
});
