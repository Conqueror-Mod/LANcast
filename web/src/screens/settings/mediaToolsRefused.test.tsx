/*
 * Harness copied from settingsRules.test.tsx: the Settings shell, admin, with
 * fetch answering from fixed values.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { Settings } from "../Settings";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

// A per-user winget folder, the shape R09-1 was found in. String.raw keeps
// the backslashes as Windows writes them.
const userDir = String.raw`C:\Users\pat\AppData\Local\Microsoft\WinGet\Links`;

const serverSettings = {
  tmdb: { configured: true },
  opensubtitles: { configured: false },
  omdb: { configured: false },
  media_tools: {
    probe_available: false,
    transcode_available: false,
    directory: userDir,
    refused: [
      {
        directory: userDir,
        reason: "the folder is owned by EXAMPLE/pat, not by Administrators or SYSTEM",
      },
    ],
  },
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
          user: { id: "u1", name: "dana", role: "admin" },
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

/*
 * A refused ffmpeg folder, on the Transcoding pane (R09-1).
 *
 * As the service, LANcast runs ffmpeg only from a folder administrators alone
 * can change. A refusal that reached only the log would leave a person looking
 * at an ffmpeg they can see beside a bare "missing", so the reason is on the
 * row, with what to do about it.
 */
describe("a refused media tools folder", () => {
  it("says which folder was not used, why, and what to do", async () => {
    await render("/settings?pane=transcoding");
    const text = host.textContent ?? "";
    expect(text).toContain("missing");
    expect(text).toContain(`Not used: ${userDir}`);
    expect(text).toContain("not by Administrators or SYSTEM");
    expect(text).toContain("LANcast.exe service tools");
    expect(host.querySelectorAll('[role="alert"]').length).toBeGreaterThan(0);
  });
});
