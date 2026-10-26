/*
 * The metrics token, in Troubleshooting (ADR 0096).
 *
 * The behaviour that matters: the token is shown once, from the answer that
 * made it; replacing and revoking ask first; and nothing is sent on the
 * first press of either.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MetricsSettings } from "./MetricsSettings";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let calls: string[];
let configured: boolean;

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  calls = [];
  configured = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      calls.push(`${method} ${url}`);
      const json = (v: unknown, status = 200) =>
        new Response(JSON.stringify(v), { status, headers: { "Content-Type": "application/json" } });
      if (String(url).includes("/api/metrics/token")) {
        if (method === "POST") {
          configured = true;
          return json({ token: "lcm_SECRET123", created_at: 1793030000 }, 201);
        }
        configured = false;
        return new Response(null, { status: 204 });
      }
      return json({ metrics_token: { configured, created_at: configured ? 1793030000 : 0 } });
    }),
  );
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
        <MetricsSettings />
      </QueryClientProvider>,
    );
  });
  await settle();
}

const button = (label: string) =>
  [...host.querySelectorAll("button")].find((b) => b.textContent === label);

async function press(label: string) {
  await act(async () => {
    button(label)!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  });
  await settle();
}

describe("metrics token", () => {
  it("makes a token and shows it once", async () => {
    await render();
    expect(host.textContent).toContain("No token yet");
    await press("Make a token");
    expect(calls).toContain("POST /api/metrics/token");
    expect(host.textContent).toContain("lcm_SECRET123");
    expect(host.textContent).toContain("isn’t shown again");
    // The settings re-read says one exists, and never carries the secret.
    expect(host.textContent).toContain("A token was made");
  });

  it("asks before revoking, and forgets the shown token", async () => {
    configured = true;
    await render();
    await press("Revoke");
    expect(calls.some((c) => c.startsWith("DELETE"))).toBe(false);
    await press("Revoke it");
    expect(calls).toContain("DELETE /api/metrics/token");
    expect(host.textContent).toContain("No token yet");
  });

  it("asks before replacing", async () => {
    configured = true;
    await render();
    await press("Replace");
    expect(calls.some((c) => c.startsWith("POST"))).toBe(false);
    expect(host.textContent).toContain("stops working at once");
    await press("Cancel");
    expect(button("Make a new token")).toBeUndefined();
    expect(calls.some((c) => c.startsWith("POST"))).toBe(false);
  });
});
