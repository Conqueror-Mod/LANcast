/*
 * Asking the provider again about one title.
 *
 * The endpoint has existed since metadata did and **nothing in this client ever
 * called it**. Correcting one wrong title meant either Fix match, which is a
 * manual search through candidates, or refreshing the whole library — about
 * 1,480 provider lookups on a real film library to fix one row.
 *
 * They are different acts and both are worth having: Fix match is for a title
 * matched to the wrong work, and this is for one whose metadata is merely
 * stale. So the assertions are that the control exists, reaches the right
 * endpoint, and says what it did — a refresh schedules work and shows nothing,
 * so without the last one its success and its failure look identical.
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

const film = {
  id: 8,
  library_id: 1,
  kind: "movie",
  title: "A Film",
  sort_title: "a film",
  year: 2019,
  missing: false,
  match_state: "matched",
  metadata_updated_at: 1_700_000_000,
};

let host: HTMLDivElement;
let root: Root;
let sent: string[];

function mount(role: string, queued = 1) {
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
      if (/\/api\/items\/\d+$/.test(url.split("?")[0])) return json(film);
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
            <MemoryRouter initialEntries={[`/item/${film.id}`]}>
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

describe("refreshing one title", () => {
  it("offers it to an admin, beside the manual correction", async () => {
    mount("admin");
    await render();
    expect(button("Refresh metadata")).toBeTruthy();
    expect(button("Fix match")).toBeTruthy();
  });

  /*
   * The endpoint is admin-only, and a button that answers 403 is worse than no
   * button: it tells somebody the fix is theirs to apply when it is not.
   */
  it("does not offer it to a member", async () => {
    mount("member");
    await render();
    expect(button("Refresh metadata")).toBeUndefined();
  });

  it("asks the item's own endpoint, not the library's", async () => {
    mount("admin");
    await render();
    await act(async () => {
      button("Refresh metadata")!.click();
    });
    await settle();

    expect(sent.some((u) => u.includes("/api/items/8/refresh"))).toBe(true);
    expect(sent.some((u) => u.includes("/api/libraries"))).toBe(false);
  });

  it("says how many titles it re-asked about", async () => {
    // The count is the point on a show: being told "1" says its episodes are
    // locked or unmatchable, which was previously invisible.
    mount("admin", 12);
    await render();
    await act(async () => {
      button("Refresh metadata")!.click();
    });
    await settle();

    expect(host.textContent).toContain("Re-asking about 12 titles");
  });

  it("says so plainly when there was nothing to re-ask about", async () => {
    mount("admin", 0);
    await render();
    await act(async () => {
      button("Refresh metadata")!.click();
    });
    await settle();

    expect(host.textContent).toContain("Nothing to re-ask about");
  });
});
