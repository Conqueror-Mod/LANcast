/*
 * The Type filter on a music library's Albums level (ADR 0115): offered only
 * when the facets carry kinds of record, a chip per kind, and a pill once one
 * is chosen. The query string half (release_type=EP) is the server's test.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { FilterBar } from "./FilterBar";
import type { Facets } from "@/api/types";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const base: Facets = {
  initials: [],
  platforms: [],
  genres: [],
  decades: [],
  content_ratings: [],
  years: [],
  resolutions: [],
  collections: [],
  max_rating: 0,
  has_watched: false,
  has_in_progress: false,
  has_unmatched: false,
};

let container: HTMLDivElement;
let root: Root;
let onToggle: ReturnType<typeof vi.fn>;

async function render(facets: Facets, params = new URLSearchParams()) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <MemoryRouter>
          <FocusProvider>
            <FilterBar
              libraryID={1}
              facets={facets}
              params={params}
              onToggle={onToggle}
              onSet={vi.fn()}
              onClear={vi.fn()}
            />
          </FocusProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
}

const category = (label: string) =>
  [...container.querySelectorAll<HTMLButtonElement>(".fbar__cat")].find((b) =>
    b.textContent?.startsWith(label),
  );

beforeEach(() => {
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
  onToggle = vi.fn();
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

describe("the Type filter", () => {
  it("is not drawn before any album is matched", async () => {
    await render({ ...base, release_types: [] });
    expect(category("Type")).toBeUndefined();
  });

  it("offers each kind of record and toggles the one pressed", async () => {
    await render({ ...base, release_types: ["Album", "EP", "Compilation"] });
    const cat = category("Type")!;
    expect(cat).toBeDefined();
    await act(async () => cat.click());
    const chips = [...container.querySelectorAll<HTMLButtonElement>(".fbar__panel .chip")];
    expect(chips.map((c) => c.textContent)).toEqual(["Album", "EP", "Compilation"]);
    await act(async () => chips[1].click());
    expect(onToggle).toHaveBeenCalledWith("release_type", "EP");
  });

  it("shows a chosen type as a pill that removes it", async () => {
    await render({ ...base, release_types: ["Album", "EP"] }, new URLSearchParams("release_type=EP"));
    const pill = container.querySelector<HTMLButtonElement>(".fbar__pill")!;
    expect(pill.getAttribute("aria-label")).toBe("Remove filter EP");
    await act(async () => pill.click());
    expect(onToggle).toHaveBeenCalledWith("release_type", "EP");
    expect(category("Type")?.querySelector(".fbar__count")?.textContent).toBe("1");
  });
});
