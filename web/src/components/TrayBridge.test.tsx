/*
 * The page's half of the tray (ADR 0080 §3): it tells the tray when a scan
 * is running, and cancels when the tray asks. jsdom has no tray; the window
 * bindings are stubbed and what is proven is what crosses them.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TrayBridge } from "./TrayBridge";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let tasks: { kind: string; state: string }[];
let cancels: number;

beforeEach(() => {
  tasks = [];
  cancels = 0;
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), { status: 200, headers: { "Content-Type": "application/json" } });
      if (url === "/api/libraries/scan/cancel" && init?.method === "POST") {
        cancels++;
        return json({ cancelled: [1] });
      }
      if (url.startsWith("/api/activity")) {
        return json({
          active: tasks.length > 0,
          completed_at: 0,
          tasks: tasks.map((t, i) => ({ ...t, id: String(i), title: "Films", done: 1, total: 2 })),
        });
      }
      return json({});
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  delete window.lancastTrayScanning;
  delete window.__lancastTray;
  vi.unstubAllGlobals();
});

async function settle() {
  for (let i = 0; i < 6; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
  }
}

async function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <TrayBridge />
      </QueryClientProvider>,
    );
  });
  await settle();
  return qc;
}

describe("tray bridge", () => {
  it("tells the tray a scan is running, and when it stops", async () => {
    const report = vi.fn();
    window.lancastTrayScanning = report;
    tasks = [{ kind: "scan", state: "running" }];
    const qc = await render();
    expect(report).toHaveBeenLastCalledWith(true);

    tasks = [];
    await act(async () => {
      await qc.invalidateQueries({ queryKey: ["activity"] });
    });
    await settle();
    expect(report).toHaveBeenLastCalledWith(false);
  });

  it("is not fooled by other running work", async () => {
    const report = vi.fn();
    window.lancastTrayScanning = report;
    tasks = [{ kind: "enrich", state: "running" }, { kind: "scan", state: "failed" }];
    await render();
    expect(report).toHaveBeenLastCalledWith(false);
  });

  it("cancels when the tray asks, as the signed-in page", async () => {
    window.lancastTrayScanning = vi.fn();
    tasks = [{ kind: "scan", state: "running" }];
    await render();
    expect(typeof window.__lancastTray).toBe("function");
    await act(async () => window.__lancastTray!("cancel-scan"));
    await settle();
    expect(cancels).toBe(1);
    // An action it does not know is ignored, never guessed at.
    await act(async () => window.__lancastTray!("delete-everything"));
    await settle();
    expect(cancels).toBe(1);
  });

  it("greys Cancel when the signed-in app goes away", async () => {
    const report = vi.fn();
    window.lancastTrayScanning = report;
    tasks = [{ kind: "scan", state: "running" }];
    await render();
    act(() => root.unmount());
    expect(report).toHaveBeenLastCalledWith(false);
    expect(window.__lancastTray).toBeUndefined();
    root = createRoot(host); // afterEach unmounts again
  });

  it("does nothing in a browser tab, where there is no tray", async () => {
    tasks = [{ kind: "scan", state: "running" }];
    await render();
    expect(window.__lancastTray).toBeUndefined();
  });
});
