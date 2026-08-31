/*
 * Turning the stars off, and — the half that matters — turning them back on.
 *
 * The flag lives on the document element, outside the tree that sets it, so a
 * missed cleanup does not fail here: it removes the stars from every screen
 * visited for the rest of the session, and the only symptom is a field that
 * quietly stopped twinkling somewhere after a detail page. That is exactly the
 * shape of this project's most-repeated bug — nothing errors, the app is
 * usable, and only the picture is wrong.
 */
import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { useStarless, STARLESS_ATTR } from "./starless";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

function Starless({ active }: { active?: boolean }) {
  useStarless(active);
  return null;
}

let host: HTMLDivElement;
let root: Root;

const flagged = () => document.documentElement.hasAttribute(STARLESS_ATTR);

beforeEach(() => {
  document.documentElement.removeAttribute(STARLESS_ATTR);
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  host.remove();
  document.documentElement.removeAttribute(STARLESS_ATTR);
});

describe("useStarless", () => {
  it("does not flag the document until something asks", () => {
    expect(flagged()).toBe(false);
  });

  it("flags the document while the screen is mounted", () => {
    act(() => root.render(<Starless />));
    expect(flagged()).toBe(true);
  });

  // Leaving this set is the failure that would be invisible: every screen after
  // the detail page would lose its stars and nothing would look broken.
  it("puts the stars back when the screen goes away", () => {
    act(() => root.render(<Starless />));
    act(() => root.unmount());
    expect(flagged(), "the flag outlived the screen that set it").toBe(false);
  });

  // Leaving and returning must not leave the flag stuck either way, which is
  // what a one-way switch would do on the second visit.
  it("survives being mounted and unmounted repeatedly", () => {
    for (let i = 0; i < 3; i++) {
      const r = createRoot(document.body.appendChild(document.createElement("div")));
      act(() => r.render(<Starless />));
      expect(flagged()).toBe(true);
      act(() => r.unmount());
      expect(flagged()).toBe(false);
    }
  });

  // Passing false is not "flag it anyway": a caller that decides per item must
  // be able to say no.
  it("does nothing when it is not active", () => {
    act(() => root.render(<Starless active={false} />));
    expect(flagged()).toBe(false);
  });
});
