/*
 * The timeline screen.
 *
 * jsdom paints nothing, so these are about wiring and about the one property
 * the feature turns on: a month's photographs are fetched **when the month is
 * opened and not before**. A screen that quietly requested all 3,676 on mount
 * would look identical, work, and be exactly the thing the buckets exist to
 * avoid.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { Timeline } from "./Timeline";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const buckets = [
  { year: 2024, month: 11, count: 3 },
  { year: 2019, month: 7, count: 2 },
  { undated: true, year: 0, month: 0, count: 5 },
];

let host: HTMLDivElement;
let root: Root;
let gets: string[];

function mount() {
  gets = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      gets.push(url);
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/timeline")) return json({ buckets, total: 10 });
      if (url.includes("/api/libraries")) {
        return json([{ id: 5, name: "Pictures", kind: "picture" }]);
      }
      if (url.includes("/api/items")) {
        return json({
          items: [
            { id: 1, title: "one", kind: "photo", library_id: 5, artwork: {} },
          ],
          total: 1,
        });
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
          <MemoryRouter initialEntries={["/library/5/timeline"]}>
            <Routes>
              <Route path="/library/:id/timeline" element={<Timeline />} />
            </Routes>
          </MemoryRouter>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  for (let i = 0; i < 5; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

describe("the photo timeline", () => {
  it("names the months it was given", async () => {
    mount();
    await render();
    expect(host.textContent).toContain("November 2024");
    expect(host.textContent).toContain("July 2019");
  });

  // 5% of a real library carries no capture time. Dropping them would lose
  // them silently; calling them "no date" says what is true.
  it("gives undated photographs a bucket of their own", async () => {
    mount();
    await render();
    expect(host.textContent).toContain("No date");
  });

  it("shows each month's count", async () => {
    mount();
    await render();
    expect(host.textContent).toContain("3");
    expect(host.textContent).toContain("10 photos");
  });

  /*
   * The property the whole design rests on. Only the newest month is open, so
   * exactly one month of photographs is requested on arrival — not three, and
   * certainly not the library.
   */
  it("fetches only the open month, not every month", async () => {
    mount();
    await render();
    const monthRequests = gets.filter((u) => u.includes("/api/items"));
    expect(
      monthRequests.length,
      `expected one month to be fetched, got ${monthRequests.length}: ${monthRequests.join(", ")}`,
    ).toBe(1);
    expect(monthRequests[0]).toContain("taken_month=2024-11");
  });

  // And opening another month fetches that one, so the laziness is a deferral
  // rather than a refusal.
  it("fetches a month when it is opened", async () => {
    mount();
    await render();

    const heads = [...host.querySelectorAll("button")].filter((b) =>
      b.textContent?.includes("July 2019"),
    );
    expect(heads.length).toBe(1);
    await act(async () => {
      heads[0].dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    for (let i = 0; i < 4; i++) {
      await act(async () => {
        await new Promise((r) => setTimeout(r, 5));
      });
    }

    expect(gets.some((u) => u.includes("taken_month=2019-07"))).toBe(true);
  });

  // The undated bucket asks for its own filter rather than a month that would
  // match nothing.
  it("asks for undated photographs by their own flag", async () => {
    mount();
    await render();

    const heads = [...host.querySelectorAll("button")].filter((b) =>
      b.textContent?.includes("No date"),
    );
    await act(async () => {
      heads[0].dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    for (let i = 0; i < 4; i++) {
      await act(async () => {
        await new Promise((r) => setTimeout(r, 5));
      });
    }

    expect(gets.some((u) => u.includes("taken_undated=1"))).toBe(true);
  });
});
