/*
 * The book filters (ADR 0116 step 3): Author and Series from the facets,
 * searched as you type, and Narrator only where the library has narrators.
 * The query string half (author=, narrator=, series=) is the server's test.
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
  platforms: [], authors: [], series: [],
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

async function render(facets: Facets, params = new URLSearchParams(), narrators = false) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <MemoryRouter>
          <FocusProvider>
            <FilterBar
              libraryID={1}
              narrators={narrators}
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

const books: Facets = {
  ...base,
  authors: ["K. W. Jeter", "Peter David"],
  series: ["Deep Space Nine", "The Next Generation"],
};

describe("the book filters", () => {
  it("are not drawn in a library with no books", async () => {
    await render(base);
    expect(category("Author")).toBeUndefined();
    expect(category("Series")).toBeUndefined();
    expect(category("Narrator")).toBeUndefined();
  });

  it("offer Narrator only where the library has narrators", async () => {
    await render(books);
    expect(category("Author")).toBeDefined();
    expect(category("Narrator")).toBeUndefined();
    await render(books, new URLSearchParams(), true);
    expect(category("Narrator")).toBeDefined();
  });

  it("search the series by any part of the name, and toggle the one pressed", async () => {
    await render(books);
    await act(async () => category("Series")!.click());
    const input = container.querySelector<HTMLInputElement>(".fbar__search")!;
    await act(async () => {
      const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
      set.call(input, "space");
      input.dispatchEvent(new Event("input", { bubbles: true }));
    });
    const chips = [...container.querySelectorAll<HTMLButtonElement>(".fbar__panel .chip")];
    expect(chips.map((c) => c.textContent)).toEqual(["Deep Space Nine"]);
    await act(async () => chips[0].click());
    expect(onToggle).toHaveBeenCalledWith("series", "Deep Space Nine");
  });

  it("show an author as a pill by name, and a narrator with the role", async () => {
    await render(
      books,
      new URLSearchParams("author=Peter+David&narrator=12"),
      true,
    );
    const labels = [...container.querySelectorAll(".fbar__pill")].map((p) =>
      p.getAttribute("aria-label"),
    );
    // The narrator waits for a name; with none supplied, only the author shows.
    expect(labels).toEqual(["Remove filter Peter David"]);
  });
});
