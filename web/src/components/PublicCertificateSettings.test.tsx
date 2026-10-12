/*
 * The public certificate section (ADR 0081).
 *
 * What matters here is what the admin is asked before the server contacts
 * anybody: the switch cannot be pressed until the CA's terms are ticked, the
 * tick is sent as the admin's answer rather than assumed, and saving again for
 * the same CA does not ask twice. jsdom sees no layout; this is wiring.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { PublicCertificateSettings } from "./PublicCertificateSettings";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const CAS = [
  { id: "letsencrypt", name: "Let's Encrypt", terms_url: "https://letsencrypt.org/repository/", trusted: true },
  { id: "letsencrypt-staging", name: "Let's Encrypt (staging, for testing)", terms_url: "https://letsencrypt.org/repository/", trusted: false },
];

const off = {
  available: true, enabled: false, hostname: "", ca: "", email: "",
  cas: CAS, status: { state: "off" },
};

let host: HTMLDivElement;
let root: Root;
let current: Record<string, unknown>;
let sent: Record<string, unknown>[];

beforeEach(() => {
  current = { ...off };
  sent = [];
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        const body = JSON.parse(String(init.body));
        sent.push(body);
        current = body.enabled
          ? { ...current, enabled: true, hostname: String(body.hostname).toLowerCase(), ca: body.ca,
              agreed_at: "2026-10-11T20:00:00Z", status: { state: "pending", hostname: body.hostname } }
          : { ...current, enabled: false, agreed_at: undefined, status: { state: "off" } };
      }
      return new Response(JSON.stringify(current), {
        status: 200, headers: { "Content-Type": "application/json" },
      });
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <PublicCertificateSettings />
      </QueryClientProvider>,
    );
  });
  await settle();
}

// The fetch, its JSON and the state update each take a turn; flush until the
// section has had all of them.
async function settle() {
  for (let i = 0; i < 5; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
  }
}

const button = (label: string) =>
  [...host.querySelectorAll("button")].find((b) => b.textContent === label) as HTMLButtonElement | undefined;

function type(el: HTMLInputElement, value: string) {
  const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
  set.call(el, value);
  el.dispatchEvent(new Event("input", { bubbles: true }));
}

describe("public certificate settings", () => {
  it("says it is unavailable on a server with no HTTPS, and offers no switch", async () => {
    current = { ...off, available: false };
    await render();
    expect(host.textContent).toContain("Unavailable");
    expect(button("Turn on")).toBeUndefined();
  });

  it("cannot be turned on until the terms are ticked, and sends the tick", async () => {
    await render();
    const name = host.querySelector('input[aria-label="Public name"]') as HTMLInputElement;
    await act(async () => type(name, "Example.DuckDNS.org"));
    expect(button("Turn on")!.disabled).toBe(true);
    expect(host.textContent).toContain("Tick the agreement above");

    const tick = host.querySelector('input[type="checkbox"]') as HTMLInputElement;
    await act(async () => tick.click());
    expect(button("Turn on")!.disabled).toBe(false);

    await act(async () => button("Turn on")!.click());
    await settle();
    expect(sent).toEqual([
      { enabled: true, hostname: "Example.DuckDNS.org", ca: "letsencrypt", email: "", agree_terms: true },
    ]);
    // The stored name is shown back, and saving again is "try now".
    expect(name.value).toBe("example.duckdns.org");
    expect(button("Save and try now")).toBeDefined();
  });

  it("does not ask for the same CA's terms twice", async () => {
    current = { ...off, enabled: true, hostname: "example.duckdns.org", ca: "letsencrypt",
      agreed_at: "2026-10-11T20:00:00Z", status: { state: "active", hostname: "example.duckdns.org",
        not_after: "2027-01-09T20:00:00Z", listening_443: true } };
    await render();
    expect(host.querySelector('input[type="checkbox"]')).toBeNull();
    expect(button("Save and try now")!.disabled).toBe(false);
    expect(host.textContent).toContain("Active for example.duckdns.org");
    expect(host.textContent).toContain("Also answering on port 443");

    // Switching CA is a different agreement.
    const select = host.querySelector("select") as HTMLSelectElement;
    await act(async () => {
      select.value = "letsencrypt-staging";
      select.dispatchEvent(new Event("change", { bubbles: true }));
    });
    expect(host.querySelector('input[type="checkbox"]')).not.toBeNull();
    expect(button("Save and try now")!.disabled).toBe(true);
    expect(host.textContent).toContain("no browser trusts them");
  });

  it("shows the CA's own words when it fails", async () => {
    current = { ...off, enabled: true, hostname: "example.duckdns.org", ca: "letsencrypt",
      agreed_at: "2026-10-11T20:00:00Z",
      status: { state: "failing", last_error: "connection refused on 443", listening_443: false,
        port_443_error: "address in use",
        next_attempt: "2026-10-11T21:00:00Z" } };
    await render();
    expect(host.querySelector('[role="alert"]')!.textContent).toContain("connection refused on 443");
    expect(host.textContent).toContain("Not answering on port 443: address in use");
  });

  it("turns off", async () => {
    current = { ...off, enabled: true, hostname: "example.duckdns.org", ca: "letsencrypt",
      agreed_at: "2026-10-11T20:00:00Z", status: { state: "active" } };
    await render();
    await act(async () => button("Turn off")!.click());
    await settle();
    expect(sent).toEqual([{ enabled: false }]);
    expect(button("Turn on")).toBeDefined();
  });
});
