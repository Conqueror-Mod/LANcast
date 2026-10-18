/*
 * "Update libraries" from the tray (ADR 0080 §3).
 *
 * The tray holds no session, so it opens Settings → Libraries with
 * ?tray=scan and the signed-in page starts the scan. What must hold: exactly
 * one scan per request, and the request gone from the address before anything
 * else can repeat it.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, useLocation } from "react-router-dom";
import { Settings } from "./Settings";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let scans: number;
let where = "";

function Where() {
  where = useLocation().search;
  return null;
}

beforeEach(() => {
  scans = 0;
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const json = (v: unknown, status = 200) =>
        new Response(JSON.stringify(v), { status, headers: { "Content-Type": "application/json" } });
      if (url === "/api/libraries/scan" && init?.method === "POST") {
        scans++;
        return json({ started: [1], busy: [] }, 202);
      }
      if (url.includes("/api/auth/status")) {
        return json({ authenticated: true, configured: true, user: { id: "u1", name: "dana", role: "admin" } });
      }
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

async function settle() {
  for (let i = 0; i < 8; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
  }
}

async function render(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={[path]}>
          <Settings />
          <Where />
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
  await settle();
  return qc;
}

describe("Update libraries from the tray", () => {
  it("starts one scan and removes the request from the address", async () => {
    await render("/settings?pane=libraries&tray=scan");
    expect(scans).toBe(1);
    expect(where).not.toContain("tray=");
    expect(where).toContain("pane=libraries");
    // The page's own note confirms it, the same as pressing the button.
    expect(host.textContent).toContain("Scanning 1 library");
  });

  it("does nothing without the request", async () => {
    await render("/settings?pane=libraries");
    expect(scans).toBe(0);
  });

  it("does not scan again when the pane re-renders", async () => {
    const qc = await render("/settings?pane=libraries&tray=scan");
    await act(async () => {
      await qc.invalidateQueries();
    });
    await settle();
    expect(scans).toBe(1);
  });
});
