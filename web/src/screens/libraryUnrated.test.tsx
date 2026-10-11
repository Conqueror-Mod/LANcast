/*
 * The "N not rated" button on a library page, rendered on the real page with
 * the real library kinds.
 *
 * This exists because the first version checked `library.kind === "rom"` and
 * a retro library's kind is "retro" — "rom" is the kind of the items in it —
 * so the button could never appear, and the page's own test passed anyway
 * because it was opened by URL against a fake library carrying the same
 * wrong kind. Here the library is shaped as the server sends it.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider } from "@/playback/PlaybackProvider";
import { LibraryView } from "./LibraryView";
import { configForKind } from "./libraryConfig";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let role: string;
let unratedAsked: number;

beforeEach(() => {
  role = "admin";
  unratedAsked = 0;
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), { status: 200, headers: { "Content-Type": "application/json" } });
      if (url.includes("/api/auth/status")) {
        return json({ authenticated: true, configured: true, user: { id: "u1", name: "chris", role } });
      }
      if (url.includes("unrated=1")) {
        unratedAsked++;
        return json({ items: [], total: 50 });
      }
      if (url.includes("/api/items")) return json({ items: [], total: 0 });
      return json({});
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

async function render(kind: string, at = "/library/9") {
  const library = { id: 9, name: "Library", kind, path: "D:/x", created_at: 1, scanned_at: 2, item_count: 1, media_count: 1 };
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <PlaybackProvider>
            <MemoryRouter initialEntries={[at]}>
              <LibraryView library={library as never} config={configForKind(kind as never)} />
            </MemoryRouter>
          </PlaybackProvider>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  for (let i = 0; i < 4; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

const notRated = () => [...host.querySelectorAll("button")].find((b) => b.textContent === "50 not rated");

describe("the not-rated button", () => {
  for (const kind of ["retro", "movie", "show"]) {
    it(`appears for an administrator on a ${kind} library`, async () => {
      await render(kind);
      expect(notRated()).toBeDefined();
    });
  }

  for (const kind of ["music", "picture"]) {
    it(`is not offered, or asked for, on a ${kind} library`, async () => {
      await render(kind);
      expect(notRated()).toBeUndefined();
      expect(unratedAsked).toBe(0);
    });
  }

  it("is not offered to a member", async () => {
    role = "member";
    await render("retro");
    expect(notRated()).toBeUndefined();
  });
});

/*
 * The phone's Filters button (docs/design.md, Phone). The stylesheet decides
 * that it shows and that the A-Z rail and chips fold away behind it; what is
 * checked here is what the button says and that it unfolds the controls.
 * Its count is the guard against a filtered grid passing for a short library.
 */
describe("the Filters button", () => {
  const toggle = () => host.querySelector<HTMLButtonElement>(".browse__filters-toggle")!;

  it("says nothing more than Filters when nothing is on", async () => {
    await render("movie");
    expect(toggle().textContent).toBe("Filters");
    expect(toggle().classList.contains("is-on")).toBe(false);
  });

  it("counts the filters that are on, the letter included", async () => {
    await render("movie", "/library/9?genre=Horror&genre=Comedy&initial=T");
    expect(toggle().textContent).toBe("Filters · 3");
    expect(toggle().classList.contains("is-on")).toBe(true);
  });

  it("does not count the search box, which stays on screen", async () => {
    await render("movie", "/library/9?q=ghost");
    expect(toggle().textContent).toBe("Filters");
  });

  it("unfolds and folds the controls", async () => {
    await render("movie");
    const controls = host.querySelector(".browse__controls")!;
    expect(controls.classList.contains("is-open")).toBe(false);
    await act(async () => toggle().click());
    expect(controls.classList.contains("is-open")).toBe(true);
    expect(toggle().getAttribute("aria-expanded")).toBe("true");
    await act(async () => toggle().click());
    expect(controls.classList.contains("is-open")).toBe(false);
  });
});
