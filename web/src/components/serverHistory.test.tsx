/*
 * Server activity on the dashboard (ADR 0106). jsdom performs no layout, so
 * the plot's width is stubbed for the hover test; what this proves is which
 * charts exist, what they say, and what is asked for.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ServerHistory } from "./ServerHistory";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let asked: string[];
let samples: unknown[];

const now = Math.floor(Date.now() / 1000);
const sample = (minAgo: number, extra: Record<string, number> = {}) => ({
  at: now - minAgo * 60, watching: 1, encoding: 1, copying: 2, cpu: -1, memory_mb: 180,
  requests: 40, errors: 0, ...extra,
});

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  asked = [];
  samples = [sample(30), sample(20, { watching: 3 }), sample(10, { encoding: 2, copying: 0 })];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      asked.push(url);
      return new Response(JSON.stringify({ every_seconds: 60, samples }), {
        headers: { "Content-Type": "application/json" },
      });
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function settle() {
  for (let i = 0; i < 4; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

async function mount() {
  await act(async () => {
    root.render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <ServerHistory />
      </QueryClientProvider>,
    );
  });
  await settle();
}

const chart = (title: string) =>
  [...host.querySelectorAll<HTMLElement>(".history__chart")].find((c) =>
    c.querySelector(".history__title span")?.textContent === title,
  )!;

describe("server activity", () => {
  it("draws one chart per measure, each with its latest value", async () => {
    await mount();
    expect(asked).toEqual(["/api/dashboard/history?hours=6"]);
    const titles = [...host.querySelectorAll(".history__title span:first-child")].map((s) => s.textContent);
    expect(titles).toEqual([
      "People watching", "Conversions running", "CPU, whole machine",
      "Memory, this server", "Requests a minute", "Server errors a minute",
    ]);
    expect(chart("Conversions running").querySelector(".history__now")?.textContent).toBe("2");
    expect(chart("Memory, this server").querySelector(".history__now")?.textContent).toBe("180 MB");
  });

  it("says CPU cannot be read here rather than drawing zeros", async () => {
    await mount();
    expect(chart("CPU, whole machine").textContent).toContain("Not readable on this machine.");
    expect(chart("CPU, whole machine").querySelector("svg")).toBeNull();
  });

  it("asks for the range chosen", async () => {
    await mount();
    const btn = [...host.querySelectorAll("button")].find((b) => b.textContent === "48 h")!;
    await act(async () => btn.click());
    await settle();
    expect(asked).toContain("/api/dashboard/history?hours=48");
    expect(btn.getAttribute("aria-pressed")).toBe("true");
  });

  it("names the moment under the pointer, with the conversions split", async () => {
    await mount();
    const svg = chart("Conversions running").querySelector("svg")!;
    svg.getBoundingClientRect = () => ({ left: 0, width: 600, top: 0, height: 90, right: 600, bottom: 90, x: 0, y: 0, toJSON() {} });
    // The right-hand end is now; the latest sample is ten minutes ago.
    act(() => {
      svg.dispatchEvent(new MouseEvent("pointermove", { bubbles: true, clientX: 600 }));
    });
    const tip = chart("Conversions running").querySelector(".history__tip");
    expect(tip?.textContent).toContain("2 re-encoding · 0 rewrapping");
  });

  it("has a table of the same numbers", async () => {
    await mount();
    const rows = chart("People watching").querySelectorAll("tbody tr");
    expect(rows.length).toBeGreaterThan(0);
    expect(chart("People watching").querySelector("tbody")?.textContent).toContain("3");
  });

  it("says plainly when nothing has been recorded yet", async () => {
    samples = [];
    await mount();
    expect(host.textContent).toContain("Nothing recorded yet");
    expect(host.querySelector(".history__chart")).toBeNull();
  });
});
