/*
 * Looking for one film's credits again.
 *
 * The only way back for a single wrong answer was re-asking the whole library —
 * a decode of every film's tail, about forty-five seconds each — and a shutdown
 * had retired two films as "unreadable" before that bug was fixed. So the
 * assertions are the same three as Refresh metadata's: the control exists for
 * the right person, reaches the item's own endpoint and not the library-wide
 * one, and says what it did.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider } from "@/playback/PlaybackProvider";
import { Detail } from "./Detail";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

function title(kind: string) {
  return {
    id: 7603,
    library_id: 1,
    kind,
    title: "Harry Potter and the Prisoner of Azkaban",
    sort_title: "harry potter",
    year: 2004,
    missing: false,
    match_state: "matched",
    metadata_updated_at: 1_700_000_000,
  };
}

let host: HTMLDivElement;
let root: Root;
let sent: string[];

function mount(role: string, kind = "movie", queued = 1) {
  sent = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const json = (body: unknown) =>
        new Response(JSON.stringify(body), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if ((init?.method ?? "GET") !== "GET") {
        sent.push(String(url));
        return json({ queued });
      }
      if (url.includes("/api/auth/status")) {
        return json({
          authenticated: true,
          configured: true,
          user: { id: "u1", name: "chris", role },
        });
      }
      if (url.includes("/children")) return json({ items: [] });
      if (/\/api\/items\/\d+$/.test(url.split("?")[0])) return json(title(kind));
      if (url.includes("/api/items")) return json({ items: [], total: 0 });
      if (url.includes("/api/libraries")) return json([]);
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
          <PlaybackProvider>
            <MemoryRouter initialEntries={["/item/7603"]}>
              <Routes>
                <Route path="/item/:id" element={<Detail />} />
              </Routes>
            </MemoryRouter>
          </PlaybackProvider>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await settle();
}

async function settle() {
  for (let i = 0; i < 3; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

function button(label: string) {
  return [...host.querySelectorAll("button")].find(
    (b) => b.textContent?.trim() === label,
  );
}

describe("re-detecting one film's credits", () => {
  it("offers it to an admin on a film", async () => {
    mount("admin");
    await render();
    expect(button("Re-detect credits")).toBeTruthy();
  });

  // The endpoint is admin-only, and a button that answers 403 tells somebody
  // the fix is theirs to apply when it is not.
  it("does not offer it to a member", async () => {
    mount("member");
    await render();
    expect(button("Re-detect credits")).toBeUndefined();
  });

  it("does not offer it on a show, which has no tail to decode", async () => {
    mount("admin", "show");
    await render();
    expect(button("Re-detect credits")).toBeUndefined();
  });

  it("asks the item's own endpoint, not the library-wide refresh", async () => {
    mount("admin");
    await render();
    await act(async () => {
      button("Re-detect credits")!.click();
    });
    await settle();

    expect(sent.some((u) => u.includes("/api/items/7603/markers/refresh"))).toBe(
      true,
    );
    expect(sent.some((u) => /\/api\/markers\/refresh/.test(u))).toBe(false);
  });

  it("says it is looking again", async () => {
    mount("admin");
    await render();
    await act(async () => {
      button("Re-detect credits")!.click();
    });
    await settle();
    expect(host.textContent).toContain("Looking for credits again");
  });

  // Zero means the server had nothing to re-ask — never examined, already
  // queued, or missing — and that must not read as success.
  it("says so when nothing was re-queued", async () => {
    mount("admin", "movie", 0);
    await render();
    await act(async () => {
      button("Re-detect credits")!.click();
    });
    await settle();
    expect(host.textContent).toContain("Credits not re-queued");
  });
});
