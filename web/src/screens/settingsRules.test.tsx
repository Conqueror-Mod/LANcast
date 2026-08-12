/*
 * The five server rules, on the settings screen.
 *
 * They have to be *reachable*, which on this screen means: on a pane a person
 * can select, rendered from the server's values, and writing back the field the
 * API documents. Panes are the part that has broken before — this screen's
 * other test file exists because a settings shell's panes were once not wired
 * to its buttons — and a control rendered into a pane nobody can reach is the
 * same failure this project has now made three times.
 *
 * Admin, because these are all admin-only surfaces; an unauthenticated render
 * shows none of them, which is what the shell test already covers.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { Settings } from "./Settings";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const serverSettings = {
  tmdb: { configured: true },
  opensubtitles: { configured: false },
  omdb: { configured: false },
  media_tools: { probe_available: true, transcode_available: true, directory: "" },
  rate_per_sec: 5,
  write_nfo: false,
  auto_enrich: true,
  update_check: true,
  encoder: {
    preference: "auto",
    active: { name: "libx264", label: "libx264", hardware: false },
    available: [],
  },
  watched_threshold: 90,
  continue_weeks: 16,
  continue_limit: 40,
  allow_media_deletion: true,
  scan_interval_hours: 0,
};

let host: HTMLDivElement;
let root: Root;
let writes: { url: string; body: unknown }[];

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  writes = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      if (method !== "GET") {
        writes.push({ url, body: init?.body ? JSON.parse(String(init.body)) : undefined });
        return new Response(JSON.stringify(serverSettings), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/api/auth/status")) {
        return json({
          authenticated: true,
          configured: true,
          user: { id: "u1", name: "chris", role: "admin" },
        });
      }
      if (url.includes("/api/settings")) return json(serverSettings);
      if (url.includes("/api/libraries")) return json([]);
      return json({ items: [], total: 0 });
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function render(entry: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={[entry]}>
          <Settings />
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
  // Two settling passes, not one: the settings query is gated on the auth
  // query, so the first flush only resolves who you are and the pane still has
  // no server values to render.
  for (let i = 0; i < 3; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

describe("server rules on the settings screen", () => {
  it("puts the viewing rules on Playback", async () => {
    await render("/settings?pane=playback");
    const text = host.textContent ?? "";
    expect(text).toContain("Counts as watched at");
    expect(text).toContain("Weeks to keep in Continue Watching");
    expect(text).toContain("Items in Continue Watching");
  });

  it("puts the library rules on Libraries", async () => {
    await render("/settings?pane=libraries");
    const text = host.textContent ?? "";
    expect(text).toContain("Rescan libraries automatically");
    expect(text).toContain("Allow deleting media files from disk");
  });

  it("renders the server's values rather than its own defaults", async () => {
    await render("/settings?pane=playback");
    const select = host.querySelector<HTMLSelectElement>("select.set-select");
    expect(select?.value).toBe("90");
    const weeks = host.querySelector<HTMLInputElement>('input[type="number"]');
    expect(weeks?.value).toBe("16");
  });

  it("writes the documented field when a rule changes", async () => {
    await render("/settings?pane=playback");
    const select = host.querySelector<HTMLSelectElement>("select.set-select")!;
    const setValue = Object.getOwnPropertyDescriptor(
      window.HTMLSelectElement.prototype,
      "value",
    )!.set!;
    await act(async () => {
      setValue.call(select, "95");
      select.dispatchEvent(new Event("change", { bubbles: true }));
    });
    const put = writes.find((w) => w.url.includes("/api/settings"));
    expect(put).toBeDefined();
    expect(put!.body).toEqual({ watched_threshold: 95 });
  });

  it("turns media deletion off with the field the API documents", async () => {
    await render("/settings?pane=libraries");
    const box = [...host.querySelectorAll<HTMLInputElement>('input[type="checkbox"]')].find(
      (b) => b.checked,
    )!;
    await act(async () => box.click());
    const put = writes.find((w) => w.url.includes("/api/settings"));
    expect(put!.body).toEqual({ allow_media_deletion: false });
  });
});
