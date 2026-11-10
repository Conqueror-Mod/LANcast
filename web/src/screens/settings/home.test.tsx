/*
 * Your account › Home (settings-3-plan step 3): each control sends what it
 * says, to the place that keeps it.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { HomePane } from "./home";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let sent: Array<{ method: string; url: string; body: unknown }>;
let hidden: string[];

const json = (v: unknown) =>
  new Response(JSON.stringify(v), { status: 200, headers: { "Content-Type": "application/json" } });

beforeEach(() => {
  sent = [];
  hidden = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      if (method !== "GET") {
        const body = init?.body ? JSON.parse(String(init.body)) : null;
        sent.push({ method, url, body });
        if (url.includes("/api/profile/home")) {
          hidden = body.hidden_shelves ?? hidden;
        } else return new Response(null, { status: 204 });
      }
      if (url.includes("/api/profile/home")) {
        return json({ account: true, spotlight: null, spotlight_item: null, spoilers: null, hidden_shelves: hidden });
      }
      if (url.includes("/api/libraries")) {
        return json([
          { id: 1, name: "Movies", kind: "movie" },
          { id: 2, name: "Music", kind: "music" },
        ]);
      }
      if (url.includes("/api/home/shelves")) {
        return json({
          max: 12,
          shelves: [
            { id: 10, library_id: 1, title: "Dramas", query: "genre=Drama", position: 0 },
            { id: 11, library_id: 1, title: "Short ones", query: "max_runtime=90", position: 1 },
          ],
        });
      }
      return json({});
    }),
  );
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  localStorage.clear();
});

async function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <HomePane />
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
  await settle();
}

async function settle() {
  for (let i = 0; i < 8; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

const toggle = (label: string) =>
  [...host.querySelectorAll<HTMLLabelElement>(".set-toggle")]
    .find((l) => l.querySelector("strong")?.textContent === label)
    ?.querySelector("input") as HTMLInputElement;

async function click(el: Element) {
  await act(async () => {
    el.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  });
  await settle();
}

describe("Your account › Home", () => {
  it("offers an Unwatched switch for film libraries only", async () => {
    await render();
    expect(toggle("Unwatched in Movies")).toBeTruthy();
    expect(toggle("Unwatched in Music")).toBeUndefined();
  });

  it("turns a shelf off on the account, and back on", async () => {
    await render();
    expect(toggle("Watchlist").checked).toBe(true);
    await click(toggle("Watchlist"));
    expect(sent.at(-1)).toMatchObject({ method: "PUT", body: { hidden_shelves: ["watchlist"] } });
    expect(toggle("Watchlist").checked).toBe(false);

    await click(toggle("Watchlist"));
    expect(sent.at(-1)).toMatchObject({ method: "PUT", body: { hidden_shelves: [] } });
  });

  it("moves and unpins a pinned shelf", async () => {
    await render();
    const up = host.querySelector('[aria-label="Move Short ones up"]')!;
    expect(host.querySelector<HTMLButtonElement>('[aria-label="Move Dramas up"]')!.disabled).toBe(true);
    await click(up);
    expect(sent.at(-1)).toMatchObject({ method: "PATCH", url: "/api/home/shelves/11", body: { position: 0 } });

    await click(host.querySelector('[aria-label="Unpin Dramas"]')!);
    expect(sent.at(-1)).toMatchObject({ method: "DELETE", url: "/api/home/shelves/10" });
  });

  it("saves the spotlight to the account", async () => {
    await render();
    const select = host.querySelector<HTMLSelectElement>('[aria-label="Homepage spotlight"]')!;
    await act(async () => {
      select.value = "recent";
      select.dispatchEvent(new Event("change", { bubbles: true }));
    });
    await settle();
    expect(sent.at(-1)).toMatchObject({ method: "PUT", body: { spotlight: "recent" } });
  });
});
