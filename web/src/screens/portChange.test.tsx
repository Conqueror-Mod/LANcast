/*
 * Moving the server to another port (ADR 0089). What is asserted is the
 * wiring and the words: the change asks once more and says what it costs,
 * sends the port only after that, follows the server to the new port, and
 * says so plainly when the server came back on the old one.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { Settings } from "./Settings";

const moved = vi.hoisted(() => ({ to: [] as string[], newAnswers: true, oldAnswers: false }));
vi.mock("@/lib/portMove", () => ({
  urlAtPort: (port: string) => `https://192.168.1.20:${port}/settings`,
  // Takes a moment, as a real check does: the page's loop waits on it.
  answersWithin: async (origin: string) => {
    await new Promise((r) => setTimeout(r, 2));
    return origin.includes(":9090") ? moved.newAnswers : moved.oldAnswers;
  },
  goTo: (url: string) => moved.to.push(url),
}));

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let network: Record<string, unknown>;
let puts: unknown[];

const base = {
  port: "8080",
  lan_enabled: true,
  secured: true,
  restart_required: false,
  addresses: ["https://192.168.1.20:8080"],
  certificate_fingerprint: "AAAA",
  custom_certificate: false,
  port_changeable: true,
};

function mount(n: Record<string, unknown>) {
  network = n;
  puts = [];
  moved.to = [];
  moved.newAnswers = true;
  moved.oldAnswers = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const json = (v: unknown, status = 200) =>
        new Response(JSON.stringify(v), { status, headers: { "Content-Type": "application/json" } });
      if (url.includes("/api/auth/status")) {
        return json({ authenticated: true, configured: true, user: { id: "u1", name: "chris", role: "admin" } });
      }
      if (url.includes("/api/network/port") && init?.method === "PUT") {
        puts.push(JSON.parse(init.body as string));
        return json({ port: "9090", restarting: true }, 202);
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
  vi.useRealTimers();
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
  await act(async () => pane!.click());
  await settle();
}

function portInput() {
  const label = [...host.querySelectorAll("label")].find((l) => l.textContent?.startsWith("Port"));
  return label!.querySelector("input") as HTMLInputElement;
}

function type(input: HTMLInputElement, value: string) {
  const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
  act(() => {
    set.call(input, value);
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
}

function button(text: string) {
  return [...host.querySelectorAll("button")].find((b) => b.textContent?.includes(text));
}

describe("moving the server to another port", () => {
  it("asks once more, says what it costs, and only then sends the port", async () => {
    mount(base);
    await render();
    expect(button("Change port")!.disabled).toBe(true);
    type(portInput(), "9090");
    await act(async () => button("Change port")!.click());
    await settle();

    expect(puts).toHaveLength(0);
    const ask = host.querySelector('[role="alertdialog"]')!;
    expect(ask.textContent).toContain("Restart the server on port 9090?");
    expect(ask.textContent).toContain("Windows Firewall has to allow port 9090");
    expect(ask.textContent).toContain("new invite");
    expect(ask.textContent).toContain("goes back to port 8080 by itself");

    await act(async () => button("Restart on port 9090")!.click());
    await settle();
    expect(puts).toEqual([{ port: 9090 }]);
    expect(moved.to).toEqual(["https://192.168.1.20:9090/settings"]);
  });

  it("says so when the server came back on the old port", async () => {
    mount(base);
    await render();
    moved.newAnswers = false;
    moved.oldAnswers = true;
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(Date.now());
    type(portInput(), "9090");
    await act(async () => button("Change port")!.click());
    await act(async () => button("Restart on port 9090")!.click());
    // Past the point where the old port is checked too.
    vi.setSystemTime(Date.now() + 40_000);
    await settle();
    expect(host.textContent).toContain("didn’t answer on port 9090, so it went back to port 8080");
    expect(moved.to).toHaveLength(0);
  });

  it("refuses its own port and anything out of range without asking", async () => {
    mount(base);
    await render();
    for (const v of ["8080", "80", "70000", "abc"]) {
      type(portInput(), v);
      expect(button("Change port")!.disabled).toBe(true);
    }
  });

  it("says why the port is fixed when this is not the installed service", async () => {
    mount({ ...base, port_changeable: false,
      port_fixed_because: "this server was not started as the installed service, so its port is the -addr it was started with" });
    await render();
    expect(host.textContent).toContain("Port 8080");
    expect(host.textContent).toContain("can’t be changed here");
    expect(button("Change port")).toBeUndefined();
  });

  it("reports a change that was rolled back", async () => {
    mount({ ...base, last_port_change: { from: "8080", to: "9090", ok: false, at: 1,
      error: "the server did not answer on port 9090, so it went back to port 8080" } });
    await render();
    expect(host.textContent).toContain("The server did not answer on port 9090, so it went back to port 8080.");
  });
});
