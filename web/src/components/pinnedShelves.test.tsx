/*
 * Pinned shelves (ADR 0119 §2): pinning sends the grid's own query, a pinned
 * shelf fetches what the grid would show, and its menu moves, renames and
 * unpins it.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { FocusProvider } from "@/focus/FocusController";
import { PinnedShelves } from "./PinnedShelves";
import { PinShelfButton, defaultShelfTitle } from "./PinShelfButton";
import { shelfQuery } from "@/lib/libraryQuery";
import type { HomeShelf, Library } from "@/api/types";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let calls: { method: string; url: string; body?: unknown }[];

const films: Library = { id: 3, name: "Films", kind: "movie" } as unknown as Library;

function stub(shelves: HomeShelf[], items: { id: number; title: string }[] = []) {
  calls = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      const method = init?.method ?? "GET";
      calls.push({ method, url, body: init?.body ? JSON.parse(String(init.body)) : undefined });
      const json = (b: unknown, status = 200) =>
        new Response(JSON.stringify(b), { status, headers: { "Content-Type": "application/json" } });
      if (method !== "GET") return method === "POST" ? json({ id: 9 }, 201) : new Response(null, { status: 204 });
      if (url.startsWith("/api/home/shelves")) return json({ shelves, max: 12 });
      if (url.startsWith("/api/items"))
        return json({
          items: items.map((i) => ({ ...i, kind: "movie", library_id: 3, artwork: {} })),
          total: items.length,
        });
      return json({});
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

async function draw(el: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <FocusProvider>
          <MemoryRouter>{el}</MemoryRouter>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await settle();
}

async function settle() {
  for (let i = 0; i < 5; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

async function click(el: Element | null | undefined) {
  expect(el).toBeTruthy();
  await act(async () => (el as HTMLElement).click());
  await settle();
}

const button = (label: string) =>
  [...document.querySelectorAll("button")].find((b) => b.textContent?.trim() === label);

const shelf = (over: Partial<HomeShelf>): HomeShelf => ({
  id: 1,
  position: 0,
  title: "Eighties action",
  library_id: 3,
  query: "decade=1980&genre=Action",
  ...over,
});

describe("pinning a view", () => {
  it("keeps only what the grid sets, in a fixed order", () => {
    const p = new URLSearchParams("page=2&genre=Action&decade=1980&sort=year&utm=x");
    expect(shelfQuery(p)).toBe("genre=Action&decade=1980&sort=year");
  });

  it("titles the shelf from its pills, and from the library when only sorted", () => {
    expect(defaultShelfTitle("Films", new URLSearchParams("genre=Action&decade=1980"))).toBe(
      "Action · 1980s",
    );
    expect(defaultShelfTitle("Films", new URLSearchParams("sort=added"))).toBe("Films");
  });

  it("sends the library and the grid's query", async () => {
    stub([]);
    await draw(
      <PinShelfButton libraryID={3} libraryName="Films" params={new URLSearchParams("genre=Action&q=die")} />,
    );
    await click(button("Pin to Home"));
    const input = host.ownerDocument.querySelector<HTMLInputElement>("#shelf-title");
    expect(input?.value).toBe("“die” · Action");
    await click(host.ownerDocument.querySelector('.shelfdlg button[type="submit"]'));
    const post = calls.find((c) => c.method === "POST");
    expect(post?.url).toBe("/api/home/shelves");
    expect(post?.body).toEqual({ library_id: 3, title: "“die” · Action", query: "genre=Action&q=die" });
    expect(button("Pinned to Home")).toBeTruthy();
  });
});

describe("a pinned shelf on Home", () => {
  it("lists what the grid would, and opens the grid with the query on", async () => {
    stub([shelf({})], [{ id: 11, title: "Die Hard" }]);
    await draw(<PinnedShelves libraries={[films]} />);
    const items = calls.find((c) => c.url.startsWith("/api/items"));
    const q = new URLSearchParams(items!.url.split("?")[1]);
    expect(q.get("library_id")).toBe("3");
    expect(q.getAll("genre")).toEqual(["Action"]);
    expect(q.getAll("decade")).toEqual(["1980"]);
    expect(host.querySelector(".section-label")?.textContent).toBe("Eighties action");
    expect(host.querySelector(".shelf__all")?.getAttribute("href")).toBe(
      "/library/3?decade=1980&genre=Action",
    );
  });

  it("narrows by your tags and favourites, the way the grid does", async () => {
    stub([shelf({ query: "tag=4&tag=11&favourite=1" })], [{ id: 11, title: "Die Hard" }]);
    await draw(<PinnedShelves libraries={[films]} />);
    const items = calls.find((c) => c.url.startsWith("/api/items"));
    const q = new URLSearchParams(items!.url.split("?")[1]);
    expect(q.getAll("tag")).toEqual(["4", "11"]);
    expect(q.get("favourite")).toBe("1");
  });

  it("is left out when its library is not one you can see", async () => {
    stub([shelf({ library_id: 99 })], [{ id: 11, title: "Die Hard" }]);
    await draw(<PinnedShelves libraries={[films]} />);
    expect(host.querySelector(".shelf")).toBeNull();
  });

  it("keeps its heading and menu when nothing matches, so it can be unpinned", async () => {
    stub([shelf({})], []);
    await draw(<PinnedShelves libraries={[films]} />);
    expect(host.querySelector(".shelf__none")).toBeTruthy();
    await click(host.querySelector(".shelf__menu"));
    await click(button("Unpin"));
    expect(calls.find((c) => c.method === "DELETE")?.url).toBe("/api/home/shelves/1");
  });

  it("moves, and cannot move past either end", async () => {
    stub(
      [shelf({ id: 1, position: 0, title: "A" }), shelf({ id: 2, position: 1, title: "B" })],
      [{ id: 11, title: "Die Hard" }],
    );
    await draw(<PinnedShelves libraries={[films]} />);
    const menus = host.querySelectorAll(".shelf__menu");
    await click(menus[0]);
    expect(button("Move up")?.disabled).toBe(true);
    await click(button("Move down"));
    expect(calls.find((c) => c.method === "PATCH")).toEqual({
      method: "PATCH",
      url: "/api/home/shelves/1",
      body: { position: 1 },
    });
    await click(host.querySelectorAll(".shelf__menu")[1]);
    expect(button("Move down")?.disabled).toBe(true);
  });

  it("renames in the page, not with prompt()", async () => {
    const prompt = vi.fn();
    vi.stubGlobal("prompt", prompt);
    stub([shelf({})], [{ id: 11, title: "Die Hard" }]);
    await draw(<PinnedShelves libraries={[films]} />);
    await click(host.querySelector(".shelf__menu"));
    await click(button("Rename"));
    const input = document.querySelector<HTMLInputElement>("#shelf-title")!;
    await act(async () => {
      const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
      set.call(input, "Rewatch");
      input.dispatchEvent(new Event("input", { bubbles: true }));
    });
    await click(document.querySelector('.shelfdlg button[type="submit"]'));
    expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ title: "Rewatch" });
    expect(prompt).not.toHaveBeenCalled();
  });
});
