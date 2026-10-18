/*
 * Status › Conversions (ADR 0087): the copy converting now shows how far it
 * has got, a stuck queue says why, and a failed copy can be tried again.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { Settings } from "./Settings";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;

let sent: string[];

function mount(list: Record<string, unknown>) {
  sent = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), { status: 200, headers: { "Content-Type": "application/json" } });
      if (url.includes("/api/auth/status")) {
        return json({ authenticated: true, configured: true, user: { id: "u1", name: "dana", role: "admin" } });
      }
      if (url.includes("/api/optimized")) return json(list);
      if (url.includes("/optimized") && init?.method) {
        sent.push(`${init.method} ${url}`);
        return new Response(null, { status: 204 });
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

async function settle() {
  for (let i = 0; i < 6; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

async function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <MemoryRouter initialEntries={["/settings"]}>
            <Settings />
          </MemoryRouter>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await settle();
  const pane = [...host.querySelectorAll("button")].find((b) => b.textContent?.trim() === "Conversions");
  expect(pane).toBeTruthy();
  await act(async () => pane!.click());
  await settle();
}

const base = { used_bytes: 4 * 2 ** 30, limit_bytes: 200 * 2 ** 30, folder: "D:\opt", current: 0, progress: 0, waiting: "" };

describe("the Conversions pane", () => {
  it("shows how far the copy converting now has got", async () => {
    mount({
      ...base,
      current: 7,
      progress: 0.4,
      items: [{ item_id: 7, title: "Dune", kind: "movie", state: "converting", size_bytes: 0, requested_at: 1 }],
    });
    await render();
    expect(host.textContent).toContain("Converting · 40%");
    expect(host.textContent).toContain("4.0 GB of 200.0 GB used");
  });

  it("says why the queue is not moving", async () => {
    mount({
      ...base,
      waiting: "waiting for space: the size limit is reached",
      items: [{ item_id: 8, title: "Heat", kind: "movie", state: "queued", size_bytes: 0, requested_at: 1 }],
    });
    await render();
    expect(host.textContent).toContain("Paused: waiting for space: the size limit is reached.");
  });

  it("tries a failed copy again", async () => {
    mount({
      ...base,
      items: [{ item_id: 9, title: "Alien", kind: "movie", state: "failed", error: "ffmpeg said no", size_bytes: 0, requested_at: 1 }],
    });
    await render();
    expect(host.textContent).toContain("Failed · ffmpeg said no");
    const retry = [...host.querySelectorAll("button")].find((b) => b.textContent === "Try again")!;
    await act(async () => retry.click());
    await settle();
    expect(sent).toContain("PUT /api/items/9/optimized");
  });
});
