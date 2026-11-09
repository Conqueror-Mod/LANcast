/*
 * The storage overview (settings-3-plan step 7): it says it is measuring until
 * a report arrives, then shows every part with its size, names what "other"
 * holds, and measures again when asked.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StorageOverview } from "./StorageOverview";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let answers: unknown[];
let asked: string[];

const report = {
  data_dir: "C:\ProgramData\LANcast",
  total_bytes: 4 * 1073741824,
  measured_at: Math.floor(Date.now() / 1000) - 120,
  took_ms: 2000,
  disk_free_bytes: 400 * 1073741824,
  disk_total_bytes: 1000 * 1073741824,
  parts: [
    { id: "database", path: "C:\ProgramData\LANcast", bytes: 90 * 1048576, files: 3 },
    { id: "artwork", path: "C:\ProgramData\LANcast\artwork", bytes: 3 * 1073741824, files: 36000 },
    { id: "optimized", path: "E:\Optimized", bytes: 0, files: 0, outside: true },
    { id: "saves", path: "C:\ProgramData\LANcast\saves", bytes: 300 * 1048576, files: 70 },
    {
      id: "other", path: "C:\ProgramData\LANcast", bytes: 89 * 1048576, files: 2,
      largest: [{ name: "lancast.db.moved-20261019", bytes: 44 * 1048576 }],
    },
  ],
};

beforeEach(() => {
  asked = [];
  answers = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      asked.push(url);
      const next = answers.length > 1 ? answers.shift() : answers[0];
      return new Response(JSON.stringify(next), { status: 200, headers: { "Content-Type": "application/json" } });
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
  vi.useRealTimers();
});

async function settle(ms = 5, n = 6) {
  for (let i = 0; i < n; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, ms));
    });
  }
}

async function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <StorageOverview />
      </QueryClientProvider>,
    );
  });
  await settle();
}

describe("the storage overview", () => {
  it("says it is measuring, and shows the report once it arrives", async () => {
    answers = [{ measuring: true, report: null }, { measuring: false, report }];
    await render();
    expect(host.textContent).toContain("Measuring the data folder");
    await settle(300, 6); // the hook asks again while measuring
    expect(host.textContent).toContain("4.0 GB in C:\ProgramData\LANcast");
    expect(host.textContent).toContain("400.0 GB free of 1000.0 GB");
    expect(host.textContent).toContain("measured 2 minutes ago");
  });

  it("shows every part, says what cannot be had again, and names what other holds", async () => {
    answers = [{ measuring: false, report }];
    await render();
    const rows = [...host.querySelectorAll(".storage__part")];
    expect(rows.map((r) => r.querySelector(".storage__label")?.textContent)).toEqual([
      "Database", "Artwork", "Optimized copies", "Game saves", "Other",
    ]);
    expect(rows[1].querySelector(".storage__size")?.textContent).toBe("3.0 GB");
    // An empty part is shown, as empty.
    expect(rows[2].classList.contains("storage__part--empty")).toBe(true);
    expect(rows[2].textContent).toContain("Kept outside the data folder, at E:\Optimized.");
    expect(rows[3].textContent).toContain("cannot be fetched or made again");
    expect(rows[4].textContent).toContain("lancast.db.moved-20261019");
    // Bars are proportions of the whole.
    const fill = rows[1].querySelector<HTMLElement>(".storage__fill")!;
    expect(fill.style.width).toBe("75%");
  });

  it("measures again when asked", async () => {
    answers = [{ measuring: false, report }];
    await render();
    answers = [{ measuring: true, report }];
    const button = [...host.querySelectorAll("button")].find((b) => b.textContent === "Measure again")!;
    await act(async () => button.click());
    await settle();
    expect(asked).toContain("/api/storage?fresh=1");
    expect(host.textContent).toContain("measuring again…");
  });
});
