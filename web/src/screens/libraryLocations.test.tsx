/*
 * A library's locations, on the settings screen (ADR 0034).
 *
 * The rules being checked are the ones a person can get wrong destructively, so
 * each is checked where a person meets it rather than in the store:
 *
 *   - the last location cannot be removed, and says why rather than vanishing
 *   - removing says how many items go with it *before* asking, because it
 *     deletes rows where every other absence on this server only marks missing
 *   - moving one location hits that location's endpoint, not the library's
 *
 * The last is the one worth a test rather than a read. `PATCH /api/libraries`
 * still accepts a path and still means something — "move the first location" —
 * so a client wiring the move control to it would work on every single-location
 * library and quietly move the wrong folder on the first library that had two.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { Settings } from "./Settings";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const twoLocations = [
  {
    id: 1,
    name: "Films",
    kind: "movie",
    path: "D:/Media/Films",
    roots: [
      { id: 1, library_id: 1, path: "D:/Media/Films", created_at: 1, item_count: 380 },
      { id: 2, library_id: 1, path: "E:/Family", created_at: 2, item_count: 32 },
    ],
    created_at: 1,
    scanned_at: 2,
    item_count: 412,
    media_count: 412,
  },
];

const oneLocation = [
  {
    ...twoLocations[0],
    roots: [twoLocations[0].roots[0]],
    item_count: 380,
  },
];

let host: HTMLDivElement;
let root: Root;
let writes: { url: string; method: string; body: unknown }[];

function mount(libraries: unknown, scan?: unknown) {
  writes = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (method !== "GET") {
        writes.push({
          url,
          method,
          body: init?.body ? JSON.parse(String(init.body)) : undefined,
        });
        return json({});
      }
      if (url.includes("/api/auth/status")) {
        return json({
          authenticated: true,
          configured: true,
          user: { id: "u1", name: "chris", role: "admin" },
        });
      }
      if (url.includes("/scan")) return json(scan ?? { state: "idle" });
      if (url.includes("/api/libraries")) return json(libraries);
      if (url.includes("/api/settings")) return json({});
      return json({ items: [], total: 0 });
    }),
  );
}

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={["/settings?pane=libraries"]}>
          <Settings />
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
  for (let i = 0; i < 4; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

function click(el: Element | null | undefined) {
  if (!el) throw new Error("nothing to click");
  act(() => {
    (el as HTMLElement).click();
  });
}

/** buttons whose text matches, across the whole rendered screen. */
function buttons(label: string): HTMLButtonElement[] {
  return [...host.querySelectorAll("button")].filter(
    (b) => b.textContent?.trim() === label,
  ) as HTMLButtonElement[];
}


/*
 * Edit, Re-read filenames, Refresh metadata and Remove moved into the row's
 * overflow menu — five buttons per library put twenty-five controls on this
 * pane. Opening the menu is the only step these tests gained; what they assert
 * afterwards is unchanged, which is the point of routing it through one helper.
 */
function openRowMenu(): void {
  const trigger = [...host.querySelectorAll("button")].find(
    (b) => b.getAttribute("aria-haspopup") === "menu",
  );
  if (!trigger) throw new Error("no overflow menu on the library row");
  act(() => trigger.click());
}

async function openEditor() {
  await render();
  openRowMenu();
  click(buttons("Edit")[0]);
  await act(async () => {
    await new Promise((r) => setTimeout(r, 5));
  });
}

describe("a library's locations", () => {
  it("lists every location with what it holds", async () => {
    mount(twoLocations);
    await openEditor();
    const inputs = [...host.querySelectorAll("input")].map((i) => i.value);
    expect(inputs).toContain("D:/Media/Films");
    expect(inputs).toContain("E:/Family");
  });

  // A library with none cannot be scanned, resolved or moved. Disabled with the
  // reason on the control rather than hidden — a missing button is a question
  // nobody can ask.
  it("will not remove the last location, and says why", async () => {
    mount(oneLocation);
    await openEditor();
    const remove = buttons("Remove").filter((b) => b.disabled);
    expect(remove.length).toBeGreaterThan(0);
    expect(remove[0].title).toMatch(/at least one location/i);
  });

  it("allows removing a location when there is more than one", async () => {
    mount(twoLocations);
    await openEditor();
    const enabled = buttons("Remove").filter((b) => !b.disabled);
    expect(enabled.length).toBeGreaterThan(0);
  });

  // Removing deletes rows, where every other absence on this server only marks
  // missing. The count has to be said before the question, not after it.
  it("names the item count before asking to remove", async () => {
    mount(twoLocations);
    await openEditor();
    const enabled = buttons("Remove").filter((b) => !b.disabled);
    click(enabled[enabled.length - 1]);
    expect(host.textContent).toMatch(/Remove 32 items\?/);
  });

  // The one that would work on every single-location library and quietly move
  // the wrong folder on the first library that had two.
  it("moves a location through that location's own endpoint", async () => {
    mount(twoLocations);
    await openEditor();

    const field = [...host.querySelectorAll("input")].find(
      (i) => i.value === "E:/Family",
    ) as HTMLInputElement;
    expect(field).toBeTruthy();

    const setter = Object.getOwnPropertyDescriptor(
      window.HTMLInputElement.prototype,
      "value",
    )!.set!;
    act(() => {
      setter.call(field, "F:/Family");
      field.dispatchEvent(new Event("input", { bubbles: true }));
    });

    click(buttons("Move").find((b) => !b.disabled));
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });

    const move = writes.find((w) => w.method === "PATCH");
    expect(move, "no PATCH was sent").toBeTruthy();
    // The location's endpoint, not the library's. /api/libraries/1 would be the
    // library-level path patch, which means "the first location".
    expect(move!.url).toContain("/api/libraries/1/roots/2");
    expect(move!.body).toEqual({ path: "F:/Family" });
  });

  it("adds a location through the roots endpoint", async () => {
    mount(twoLocations);
    await openEditor();

    const field = [...host.querySelectorAll("input")].find(
      (i) => i.placeholder?.startsWith("Add another folder"),
    ) as HTMLInputElement;
    const setter = Object.getOwnPropertyDescriptor(
      window.HTMLInputElement.prototype,
      "value",
    )!.set!;
    act(() => {
      setter.call(field, "G:/Archive");
      field.dispatchEvent(new Event("input", { bubbles: true }));
    });

    click(buttons("Add location")[0]);
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });

    const add = writes.find((w) => w.method === "POST");
    expect(add, "no POST was sent").toBeTruthy();
    expect(add!.url).toContain("/api/libraries/1/roots");
    expect(add!.body).toEqual({ path: "G:/Archive" });
  });
});

describe("a scan that could not read every location", () => {
  /*
   * The scan succeeded and looks it, while having covered less of the library
   * than it appears to. Nothing else on this screen would say so — which is the
   * same failure the wrong-kind warning exists for, and the reason this is in
   * the row rather than behind the issues toggle.
   */
  it("says how many locations were read and which was not", async () => {
    mount(twoLocations, {
      state: "idle",
      files_seen: 380,
      skipped: 0,
      skipped_kind: 0,
      roots_scanned: 1,
      roots_skipped: [{ id: 2, path: "E:/Family" }],
    });
    await render();
    expect(host.textContent).toMatch(/1 of 2 locations scanned/);
    expect(host.textContent).toContain("E:/Family");
    // And says the rows were left alone, because "could not read" otherwise
    // reads as "lost".
    expect(host.textContent).toMatch(/not marked missing/i);
  });

  it("says nothing when every location was read", async () => {
    mount(twoLocations, {
      state: "idle",
      files_seen: 412,
      skipped: 0,
      skipped_kind: 0,
      roots_scanned: 2,
      roots_skipped: [],
    });
    await render();
    expect(host.textContent).not.toMatch(/locations scanned/);
  });
});

describe("browsing for a folder", () => {
  /*
   * An absolute server path typed from memory is the one field on this screen a
   * person cannot check as they go: a typo is accepted, stored, and only shows
   * up later as a location that scans nothing. The picker already existed for
   * adding a library; these assert it is reachable from the two places that
   * take a path per *location*.
   */
  it("offers Browse when adding a location", async () => {
    mount(twoLocations);
    await openEditor();
    const browse = buttons("Browse…");
    expect(browse.length).toBeGreaterThan(0);
  });

  it("offers Browse on each existing location, for moving it", async () => {
    mount(twoLocations);
    await openEditor();
    // One per location, plus the one on the add row.
    expect(buttons("Browse…").length).toBe(twoLocations[0].roots.length + 1);
  });
});
