/*
 * The re-read filenames button, on the settings screen.
 *
 * Two things are worth a test rather than a read, and both are failures this
 * project has already shipped once:
 *
 *   - the button must hit /reparse, not /refresh. They sit next to each other,
 *     take the same argument, and both "work" — a control wired to the wrong one
 *     would re-ask the provider the same question for ever and look fine doing
 *     it, since the visible outcome of either is "something happened".
 *   - the outcome must distinguish a repair from a no-op. "0 changed" and
 *     "nothing left to examine" mean opposite things, and a success state that
 *     reads identically to a no-op state is the exact failure the settings shell
 *     tests were written for.
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

const libraries = [
  {
    id: 7,
    name: "Films",
    kind: "movie",
    path: "D:/Media/Films",
    roots: [
      { id: 1, library_id: 7, path: "D:/Media/Films", created_at: 1, item_count: 380 },
    ],
    created_at: 1,
    scanned_at: 2,
    item_count: 380,
    media_count: 380,
  },
];

let host: HTMLDivElement;
let root: Root;
let writes: { url: string; method: string }[];

function mount(reparse: { examined: number; changed: number }) {
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
        writes.push({ url, method });
        if (url.includes("/reparse")) return json(reparse);
        return json({});
      }
      if (url.includes("/api/auth/status")) {
        return json({
          authenticated: true,
          configured: true,
          user: { id: "u1", name: "chris", role: "admin" },
        });
      }
      if (url.includes("/scan")) return json({ state: "idle" });
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

async function pressReparse() {
  openRowMenu();
  const b = buttons("Re-read filenames")[0];
  if (!b) throw new Error("no re-read filenames button on the libraries pane");
  act(() => b.click());
  for (let i = 0; i < 4; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

describe("re-read filenames", () => {
  it("posts to reparse, not refresh", async () => {
    mount({ examined: 160, changed: 98 });
    await render();
    await pressReparse();

    const posted = writes.filter((w) => w.method === "POST");
    expect(posted.map((w) => w.url)).toContain("/api/libraries/7/reparse");
    expect(posted.some((w) => w.url.includes("/refresh"))).toBe(false);
  });

  it("says how many were corrected", async () => {
    mount({ examined: 160, changed: 98 });
    await render();
    await pressReparse();

    expect(host.textContent).toContain("160");
    expect(host.textContent).toContain("98");
  });

  // The distinction the counts exist for: nothing needed fixing is not the same
  // answer as nothing was left to look at.
  it("tells a no-op apart from a run that found nothing to fix", async () => {
    mount({ examined: 0, changed: 0 });
    await render();
    await pressReparse();
    expect(host.textContent).toContain("already been re-read");

    act(() => root.unmount());
    host.remove();
    host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);

    mount({ examined: 12, changed: 0 });
    await render();
    await pressReparse();
    expect(host.textContent).toContain("already matched their filenames");
  });
});
