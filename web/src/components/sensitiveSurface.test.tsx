/*
 * Where a cover may be lifted (ADR 0051, amended).
 *
 * Reported: accepting a folder uncovered it everywhere for the rest of the
 * session — the home page included. So the screen somebody else is most likely
 * to be glancing at was also the one where a single click uncovered the folder,
 * and it stayed uncovered.
 *
 * Acknowledgement is now a property of *where you are* rather than of the item.
 * These are the two halves of that: a surface that has not opted in cannot lift
 * a cover however hard it is pressed, and one that has still can.
 */
import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PosterTile } from "./PosterTile";
import { SensitiveReveal, forgetAcknowledgements } from "@/lib/sensitiveAck";
import type { Item } from "@/api/types";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const folder = {
  id: 7,
  library_id: 1,
  kind: "gallery",
  title: "A Folder",
  parent_id: null,
  artwork: { poster: "abc123" },
  sensitive: true,
  sensitive_own: true,
} as Item;

let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  forgetAcknowledgements();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  forgetAcknowledgements();
});

function render(allowed: boolean) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const tile = <PosterTile item={folder} />;
  act(() => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <MemoryRouter>
            {allowed ? <SensitiveReveal>{tile}</SensitiveReveal> : tile}
          </MemoryRouter>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
}

function press() {
  act(() => {
    host
      .querySelector("button")!
      .dispatchEvent(new MouseEvent("click", { bubbles: true }));
  });
}

describe("a surface that has not opted in", () => {
  // The reported fault. This is the home page, the shelves, the hero, search
  // and collections — everything that did not say otherwise.
  it("cannot be clicked into showing the picture", () => {
    render(false);
    expect(host.querySelector("img")).toBeNull();
    press();
    expect(
      host.querySelector("img"),
      "a cover was lifted on a surface that may not lift covers",
    ).toBeNull();
  });

  // And it does not invite the press either, because an invitation that does
  // nothing reads as a broken tile rather than as a decision.
  it("does not offer to show it", () => {
    render(false);
    expect(host.textContent).toContain("Sensitive");
    expect(host.textContent).not.toContain("click to show");
  });

  // The name still shows: a folder you marked is one you must be able to find.
  it("still shows the name", () => {
    render(false);
    expect(host.textContent).toContain("A Folder");
  });
});

describe("a surface that has opted in", () => {
  it("offers to show it, and does", () => {
    render(true);
    expect(host.textContent).toContain("click to show");
    press();
    expect(host.querySelector("img")).not.toBeNull();
  });
});

/*
 * And leaving the pictures forgets it.
 *
 * The acknowledgement is meant to last a visit, not a session: coming back to
 * the home page later and finding the folder still uncovered is the same fault
 * as never having covered it.
 */
describe("leaving", () => {
  it("re-covers what was accepted once the surface goes away", async () => {
    render(true);
    press();
    expect(host.querySelector("img")).not.toBeNull();

    // Unmount the permitting surface, then let the deferred clear run.
    act(() => root.render(<div />));
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });

    render(true);
    expect(
      host.querySelector("img"),
      "the acknowledgement outlived the visit it belonged to",
    ).toBeNull();
  });
});
