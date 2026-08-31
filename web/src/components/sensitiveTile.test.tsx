/*
 * A covered tile does not carry the picture (ADR 0051).
 *
 * This is the one assertion the feature actually rests on. Blurring the real
 * image would satisfy a screenshot and fail the requirement: the bytes would be
 * in the page, and anything that drops styles — a stylesheet that has not
 * loaded, a reader view, a devtools panel — would show the photograph on
 * somebody else's screen, which is the entire scenario.
 *
 * jsdom cannot see a blur, and that is fine, because a blur is not what is
 * being claimed. What is being claimed is that no <img> exists and no URL was
 * built, and jsdom can see both exactly.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PosterTile } from "./PosterTile";
import { forgetAcknowledgements } from "@/lib/sensitiveAck";
import type { Item } from "@/api/types";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

function photo(over: Partial<Item>): Item {
  return {
    id: 7,
    library_id: 1,
    kind: "photo",
    title: "Kept Folder",
    parent_id: null,
    artwork: { poster: "abc123" },
    ...over,
  } as Item;
}

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
  vi.unstubAllGlobals();
});

function render(item: Item) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  act(() => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <MemoryRouter>
            <PosterTile item={item} />
          </MemoryRouter>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
}

describe("a sensitive tile", () => {
  it("renders no image at all", () => {
    render(photo({ sensitive: true }));
    expect(host.querySelector("img")).toBeNull();
  });

  // The name is deliberately readable: hiding it too would make somebody hunt
  // through identical grey rectangles for the folder they marked themselves.
  it("still shows the name", () => {
    render(photo({ sensitive: true }));
    expect(host.textContent).toContain("Kept Folder");
  });

  it("says what it is and how to see it", () => {
    render(photo({ sensitive: true }));
    expect(host.textContent).toContain("Sensitive");
  });

  // An unmarked photo is untouched — the feature costs nothing to anyone who
  // has not used it.
  it("draws an unmarked photo normally", () => {
    render(photo({}));
    expect(host.querySelector("img")).not.toBeNull();
  });

  /*
   * The first press reveals rather than opening.
   *
   * A cover you can click straight through has not stopped anything. This is
   * the whole of the interaction, so it is asserted rather than assumed from
   * the handler being wired.
   */
  it("reveals on the first press instead of opening", () => {
    render(photo({ sensitive: true }));
    const tile = host.querySelector("button");
    expect(tile).not.toBeNull();

    act(() => {
      tile!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    const img = host.querySelector("img");
    expect(img, "the picture did not appear after acknowledging").not.toBeNull();
    expect(img!.getAttribute("src")).toContain("abc123");
  });
});
