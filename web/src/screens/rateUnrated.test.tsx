/*
 * Rating by hand: the page that lists what a ceiling hides in a library, and
 * the picker on it. The server is stubbed at fetch, holding a list that
 * shrinks when something is rated, so these prove the wiring: the right label
 * goes to the right item, the row leaves once the server says it is rated,
 * and only an administrator is offered any of it.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { RateUnrated } from "./RateUnrated";
import { ratingChoices } from "@/lib/ratings";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let admin: boolean;
let unrated: { id: number; kind: string; title: string; platform: string; year: number }[];
let patches: { url: string; body: unknown }[];

beforeEach(() => {
  admin = true;
  unrated = [
    { id: 41, kind: "rom", title: "Super Mario 64", platform: "n64", year: 1996 },
    { id: 42, kind: "rom", title: "Conker's Bad Fur Day", platform: "n64", year: 2001 },
  ];
  patches = [];
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const u = String(url);
      const method = init?.method ?? "GET";
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), { status: 200, headers: { "Content-Type": "application/json" } });
      if (method === "PATCH") {
        const body = JSON.parse(String(init?.body));
        patches.push({ url: u, body });
        const id = Number(u.split("/").pop());
        unrated = unrated.filter((g) => g.id !== id);
        return json({ id, content_rating: body.content_rating });
      }
      if (u.includes("unrated=1")) return json({ items: unrated, total: unrated.length });
      if (u.includes("/api/auth")) {
        return json({ user: { role: admin ? "admin" : "user" }, can_convert: true, configured: true, authenticated: true });
      }
      if (u.includes("/api/libraries")) return json([{ id: 9, name: "Retro Games", kind: "rom" }]);
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

async function flush() {
  for (let i = 0; i < 3; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 10));
    });
  }
}

async function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <MemoryRouter initialEntries={["/library/9/unrated"]}>
            <Routes>
              <Route path="/library/:id/unrated" element={<RateUnrated />} />
            </Routes>
          </MemoryRouter>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await flush();
}

function row(title: string): HTMLElement | undefined {
  return [...host.querySelectorAll<HTMLElement>(".unrated__row")].find((r) =>
    r.querySelector(".unrated__name")?.textContent?.includes(title),
  );
}

describe("rating what a ceiling hides", () => {
  it("lists each unrated game with its console and year, and how many are left", async () => {
    await render();
    expect(row("Super Mario 64")?.textContent).toContain("Nintendo 64 · 1996");
    expect(host.textContent).toContain("2 left");
  });

  it("sends the chosen ESRB rating, with its system's name, and the game leaves the list", async () => {
    await render();
    const e = [...row("Super Mario 64")!.querySelectorAll("button")].find((b) => b.textContent === "E")!;
    await act(async () => e.click());
    await flush();
    expect(patches).toEqual([{ url: "/api/items/41", body: { content_rating: "ESRB E" } }]);
    expect(row("Super Mario 64")).toBeUndefined();
    expect(row("Conker's Bad Fur Day")).toBeDefined();
    expect(host.textContent).toContain("1 left");
  });

  it("says when nothing is left", async () => {
    unrated = [];
    await render();
    expect(host.textContent).toContain("Everything here is rated");
  });

  it("offers nothing to an account that is not an administrator", async () => {
    admin = false;
    await render();
    expect(host.querySelectorAll(".unrated__row")).toHaveLength(0);
    expect(host.textContent).toContain("Only an administrator");
  });
});

describe("what can be chosen", () => {
  it("offers ESRB for a game, TV ratings for television, film certificates for a film", () => {
    expect(ratingChoices("rom")).toEqual(["ESRB EC", "ESRB E", "ESRB E10+", "ESRB T", "ESRB M", "ESRB AO"]);
    expect(ratingChoices("episode")[0]).toBe("TV-Y");
    expect(ratingChoices("movie")).toContain("PG-13");
  });

  it("keeps a rating the item already carries even if it is not offered", () => {
    expect(ratingChoices("rom", "ESRB KA")[0]).toBe("ESRB KA");
  });
});
