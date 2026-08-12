/*
 * The playlists page: is it wired to anything?
 *
 * The failure this guards against is the one this whole feature keeps making —
 * a control that renders and leads nowhere. So these assert the *requests*: that
 * the page asks for playlists in this library rather than all of them, and that
 * creating one actually posts. What the tiles look like is not the subject; that
 * they are made from real data, and that the buttons on them go somewhere, is.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { Playlists } from "./Playlists";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const playlists = [
  { id: 31, library_id: 3, kind: "playlist", title: "Road Trip", child_count: 3 },
  { id: 32, library_id: 3, kind: "playlist", title: "The Gym One", child_count: 1 },
];

let host: HTMLDivElement;
let root: Root;
let requests: { method: string; url: string; body: unknown }[];

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  requests = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      requests.push({
        method,
        url,
        body: init?.body ? JSON.parse(String(init.body)) : undefined,
      });
      // /api/libraries answers with a bare array, not a page.
      if (method === "GET" && url.includes("/api/libraries")) {
        return new Response(
          JSON.stringify([{ id: 3, name: "Music", kind: "music" }]),
          { status: 200, headers: { "Content-Type": "application/json" } },
        );
      }
      if (method === "GET" && url.includes("kind=playlist")) {
        return new Response(
          JSON.stringify({ items: playlists, total: playlists.length }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        );
      }
      if (method === "GET" && url.includes("playlist_id=")) {
        return new Response(JSON.stringify({ items: [], total: 0 }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (method === "POST") {
        return new Response(
          JSON.stringify({ id: 99, kind: "playlist", title: "Tonight" }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        );
      }
      return new Response(JSON.stringify({ items: [], total: 0 }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }),
  );
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
        <MemoryRouter initialEntries={["/library/3/playlists"]}>
          <FocusProvider>
            <Routes>
              <Route path="/library/:id/playlists" element={<Playlists />} />
              <Route path="/item/:id" element={<div id="detail" />} />
            </Routes>
          </FocusProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
  // The queries resolve a microtask later; without this the assertions run
  // against the loading state and every one of them reads as "nothing rendered".
  await flush();
}

// Let pending promises settle inside act, so React has applied the results.
async function flush() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}

function button(label: RegExp): HTMLButtonElement {
  const found = [...host.querySelectorAll<HTMLButtonElement>("button")].find(
    (b) => label.test(b.getAttribute("aria-label") ?? b.textContent ?? ""),
  );
  if (!found) throw new Error(`no button matching ${label}`);
  return found;
}

describe("playlists page", () => {
  it("asks for this library's playlists, not every playlist", async () => {
    await render();
    const listing = requests.find((r) => r.url.includes("kind=playlist"));
    expect(listing).toBeDefined();
    // A playlist belongs to a library, and this is a page of one.
    expect(listing!.url).toContain("library_id=3");
  });

  it("renders a tile per playlist with its entry count", async () => {
    await render();
    const text = host.textContent ?? "";
    expect(text).toContain("Road Trip");
    expect(text).toContain("3 tracks");
    // Singular, because "1 tracks" is the kind of detail that makes a screen
    // look machine-generated.
    expect(text).toContain("1 track");
    expect(text).not.toContain("1 tracks");
  });

  it("creates a playlist in this library and opens it", async () => {
    await render();
    await act(async () => button(/New playlist/).click());
    const input = host.querySelector<HTMLInputElement>(".pl-prompt__input")!;
    // React tracks the input's value on the node, so assigning `.value`
    // directly is invisible to it — the native setter is what makes onChange
    // fire and the Create button leave its disabled state.
    const setValue = Object.getOwnPropertyDescriptor(
      window.HTMLInputElement.prototype,
      "value",
    )!.set!;
    await act(async () => {
      setValue.call(input, "Tonight");
      input.dispatchEvent(new Event("input", { bubbles: true }));
    });
    await act(async () => button(/^Create$/).click());
    await flush();

    const post = requests.find((r) => r.method === "POST");
    expect(post).toBeDefined();
    expect(post!.url).toBe("/api/playlists");
    expect(post!.body).toEqual({ title: "Tonight", library_id: 3 });
    // And it goes into the new playlist, since the useful next step is filling
    // it and that happens on its own page.
    expect(host.querySelector("#detail")).not.toBeNull();
  });

  it("offers rename and delete per playlist", async () => {
    await render();
    expect(button(/^Rename Road Trip$/)).toBeTruthy();
    expect(button(/^Delete Road Trip$/)).toBeTruthy();
  });
});
