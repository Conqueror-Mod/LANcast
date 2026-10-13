/*
 * Server › Network: a status, not a control. The sentence at the top is the
 * point of the pane, so each state of the loopback rule is asserted to say
 * what it is — and that there is no switch for it anywhere on the pane.
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

let network: Record<string, unknown>;

function mount(n: Record<string, unknown>) {
  network = n;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), { status: 200, headers: { "Content-Type": "application/json" } });
      if (url.includes("/api/auth/status")) {
        return json({ authenticated: true, configured: true, user: { id: "u1", name: "chris", role: "admin" } });
      }
      if (url.includes("/api/network")) return json(network);
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
  const pane = [...host.querySelectorAll("button")].find((b) => b.textContent?.trim() === "Network");
  expect(pane).toBeTruthy();
  await act(async () => pane!.click());
  await settle();
}

const base = {
  port: "8080",
  lan_enabled: false,
  secured: true,
  restart_required: false,
  addresses: [],
  certificate_fingerprint: "",
  custom_certificate: false,
};

describe("the Network pane", () => {
  it("says where the server is reachable, and lists every address", async () => {
    mount({
      ...base,
      lan_enabled: true,
      addresses: ["https://192.168.1.20:8080", "https://[fd00::20]:8080"],
      certificate_fingerprint: "sha256/abc",
    });
    await render();
    expect(host.textContent).toContain("Reachable on your network at https://192.168.1.20:8080");
    expect(host.textContent).toContain("https://[fd00::20]:8080");
    expect(host.textContent).toContain("sha256/abc");
  });

  it("says why only this computer can reach it when no password is set", async () => {
    mount({ ...base, secured: false });
    await render();
    expect(host.textContent).toContain("because no password is set");
  });

  it("says a restart is owed after a password was set", async () => {
    mount({ ...base, restart_required: true });
    await render();
    expect(host.textContent).toContain("opens the next time the server restarts");
  });

  it("offers no switch for the loopback rule", async () => {
    mount({ ...base, secured: false });
    await render();
    const section = [...host.querySelectorAll(".settings__section")].find((s) =>
      s.textContent?.includes("This machine and your network"),
    );
    expect(section?.querySelectorAll("input, select, button")).toHaveLength(0);
  });
});
