/*
 * Searching photographs by description, and the three empty screens it must
 * keep apart.
 *
 * A grid with nothing in it means one of: the models are not installed, this
 * library has never been indexed, or it was searched and nothing matched. They
 * are the same empty grid and three completely different sentences, with three
 * different fixes — download something, press a button, type something else.
 * Telling somebody the wrong one is how a working feature gets reported as
 * broken, and this project has already paid for it once on the people page.
 *
 * jsdom paints nothing, so these are about which sentence is shown and what is
 * asked of the server — not about how the results look.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PhotoSearch } from "./PhotoSearch";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

type Scenario = {
  ready: boolean;
  reason?: string;
  indexed?: number;
  hits?: { item: { id: number; title: string }; score: number }[];
};

let host: HTMLDivElement;
let root: Root;
let gets: string[];
let sent: string[];

function mount(s: Scenario) {
  gets = [];
  sent = [];
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
        sent.push(url);
        return json({ ok: true });
      }
      gets.push(url);
      if (url.includes("/api/auth/status")) {
        return json({
          authenticated: true,
          configured: true,
          user: { id: "u1", name: "chris", role: "admin" },
        });
      }
      if (url.includes("/photos/semantic/capabilities")) {
        return json({
          semantic_ready: s.ready,
          semantic_reason: s.reason,
          semantic_model: s.ready ? "openclip-vit-b-32" : undefined,
        });
      }
      if (url.includes("/photos/search")) {
        return json({
          hits: s.hits ?? [],
          indexed: s.indexed ?? 0,
          model: "openclip-vit-b-32",
        });
      }
      if (url.includes("/api/libraries")) {
        return json([{ id: 5, name: "Pictures", kind: "picture" }]);
      }
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

async function render() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <MemoryRouter initialEntries={["/library/5/photos/search"]}>
            <Routes>
              <Route
                path="/library/:id/photos/search"
                element={<PhotoSearch />}
              />
            </Routes>
          </MemoryRouter>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await settle();
}

async function settle() {
  for (let i = 0; i < 6; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

function text() {
  return host.textContent ?? "";
}

/** Type a query and submit it, the way a person does. */
async function search(q: string) {
  const input = host.querySelector("input") as HTMLInputElement;
  const form = host.querySelector("form") as HTMLFormElement;
  await act(async () => {
    const setter = Object.getOwnPropertyDescriptor(
      window.HTMLInputElement.prototype,
      "value",
    )!.set!;
    setter.call(input, q);
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
  await act(async () => {
    form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
  });
  await settle();
}

describe("searching photographs by description", () => {
  /*
   * State one: nothing installed.
   *
   * The server's own reason is shown, because "not installed" and "no model"
   * are different problems with different fixes — and the field is disabled, so
   * nobody types a query into a page that cannot answer it.
   */
  it("says the feature is not set up, and why, rather than showing an empty grid", async () => {
    mount({ ready: false, reason: "no semantic model is installed" });
    await render();

    expect(text()).toContain("not set up");
    expect(text()).toContain("no semantic model is installed");
    expect(text()).not.toContain("Nothing matched");

    const input = host.querySelector("input") as HTMLInputElement;
    expect(input.disabled).toBe(true);
  });

  /*
   * And it does not ask the server to search.
   *
   * A disabled field is a hint; not issuing the request is the behaviour. The
   * server would answer 409, and a failed request drawn as an error would say
   * something went wrong when nothing has.
   */
  it("does not search when the models are missing", async () => {
    mount({ ready: false, reason: "not installed" });
    await render();

    expect(gets.some((u) => u.includes("/photos/search"))).toBe(false);
  });

  /*
   * State two: installed, but this library has no vectors.
   *
   * This is the state where "nothing matched" would be an outright lie — the
   * library was never looked at. It must survive a search returning zero hits,
   * which is exactly what the server does answer here.
   */
  it("says the library has not been indexed rather than that nothing matched", async () => {
    mount({ ready: true, indexed: 0, hits: [] });
    await render();
    await search("a dog on a beach");

    expect(text()).toContain("has been indexed");
    expect(text()).not.toContain("Nothing matched");
  });

  /*
   * State three, and the only one that really is "no results". It says how many
   * photographs were searched, because "nothing matched" over an indexed
   * library and over an empty one read identically without it.
   */
  it("says nothing matched, and how many were searched, when the library is indexed", async () => {
    mount({ ready: true, indexed: 1206, hits: [] });
    await render();
    await search("a dog on a beach");

    expect(text()).toContain("Nothing matched");
    expect(text()).toContain("1,206");
  });

  // Results are shown best-first with the count, and the query actually
  // reaches the server encoded rather than raw.
  it("asks the server for the typed query and shows what came back", async () => {
    mount({
      ready: true,
      indexed: 900,
      hits: [
        { item: { id: 11, title: "IMG_0001" }, score: 0.31 },
        { item: { id: 12, title: "IMG_0002" }, score: 0.28 },
      ],
    });
    await render();
    await search("a dog on a beach");

    const asked = gets.find((u) => u.includes("/photos/search"));
    expect(asked).toBeTruthy();
    expect(asked).toContain("q=a%20dog%20on%20a%20beach");
    expect(text()).toContain("2 of 900");
  });

  /*
   * Nothing is asked until the query is submitted.
   *
   * Every search is a process start and a model load on the server. Searching
   * per keystroke would spend six of those to answer one question, and the
   * answer to a half-typed query ranks confidently and wrongly — which reads as
   * the feature being bad rather than the query being unfinished.
   */
  it("does not search while the query is still being typed", async () => {
    mount({ ready: true, indexed: 900, hits: [] });
    await render();

    const input = host.querySelector("input") as HTMLInputElement;
    await act(async () => {
      const setter = Object.getOwnPropertyDescriptor(
        window.HTMLInputElement.prototype,
        "value",
      )!.set!;
      setter.call(input, "a dog");
      input.dispatchEvent(new Event("input", { bubbles: true }));
    });
    await settle();

    expect(gets.some((u) => u.includes("/photos/search"))).toBe(false);
  });

  /*
   * A fourth state, and the one that really does read as broken: a server that
   * could not be asked at all.
   *
   * Found by pointing the page at a server too old to have the route. Every
   * other branch is guarded on having an answer, so a 404 left a disabled field
   * and no explanation of any kind.
   */
  it("says so when the server could not be asked whether it can search", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string) => {
        const json = (v: unknown) =>
          new Response(JSON.stringify(v), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          });
        if (url.includes("/photos/semantic/capabilities")) {
          return new Response("not found", { status: 404 });
        }
        if (url.includes("/api/libraries")) {
          return json([{ id: 5, name: "Pictures", kind: "picture" }]);
        }
        return json({});
      }),
    );
    await render();

    expect(host.textContent).toContain("could not be asked");
  });

  // The indexing pass is offered only when the models are here, because a
  // button that always fails teaches people the feature is broken.
  it("offers indexing only when the models are installed", async () => {
    mount({ ready: false, reason: "not installed" });
    await render();
    expect(text()).not.toContain("Index photographs");

    await act(async () => root.unmount());
    root = createRoot(host);
    mount({ ready: true, indexed: 0 });
    await render();
    expect(text()).toContain("Index photographs");
  });
});
