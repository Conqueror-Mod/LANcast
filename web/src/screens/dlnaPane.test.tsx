/*
 * Server › DLNA (ADR 0086): the status line says why it is not running, and
 * the account a TV browses as is sent to the server when chosen.
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

let sent: Record<string, unknown>[];

function mount(status: Record<string, unknown>) {
  sent = [];
  const settings: Record<string, unknown> = { dlna_enabled: true, dlna_account: "", dlna_name: "" };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), { status: 200, headers: { "Content-Type": "application/json" } });
      if (url.includes("/api/auth/status")) {
        return json({ authenticated: true, configured: true, user: { id: "u1", name: "dana", role: "admin" } });
      }
      if (url.includes("/api/dlna")) return json(status);
      if (url.endsWith("/api/users")) {
        return json({ users: [{ id: "u1", name: "dana", role: "admin" }, { id: "u_kid", name: "Kids", role: "member" }] });
      }
      if (url.includes("/api/settings")) {
        if (init?.method === "PUT" || init?.method === "PATCH") {
          const body = JSON.parse(String(init.body));
          sent.push(body);
          Object.assign(settings, body);
        }
        return json(settings);
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
  const pane = [...host.querySelectorAll("button")].find((b) => b.textContent?.trim() === "DLNA");
  expect(pane).toBeTruthy();
  await act(async () => pane!.click());
  await settle();
}

describe("the DLNA pane", () => {
  it("says why it is not running", async () => {
    mount({ running: false, reason: "no account chosen", port: 8200 });
    await render();
    expect(host.textContent).toContain("Not running: no account chosen.");
    expect(host.textContent).toContain("-LocalPort 8200");
  });

  it("sends the account a TV browses as", async () => {
    mount({ running: false, reason: "no account chosen", port: 8200 });
    await render();
    const select = [...host.querySelectorAll("select")].find((s) =>
      [...s.options].some((o) => o.textContent === "Kids"),
    )!;
    await act(async () => {
      select.value = "u_kid";
      select.dispatchEvent(new Event("change", { bubbles: true }));
    });
    await settle();
    expect(sent).toContainEqual({ dlna_account: "u_kid" });
  });

  it("says when it is running, under the name TVs list", async () => {
    mount({ running: true, reason: "", port: 8200 });
    await render();
    expect(host.textContent).toContain('Running. TVs on your network can find "LANcast".');
  });
});
