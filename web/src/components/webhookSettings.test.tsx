/*
 * Server › Webhooks (ADR 0097).
 *
 * The wiring that matters: the form sends the address and the events picked;
 * the secret is shown once, from the answer that made it; a failing webhook
 * says so on its row; Remove asks twice; and the consent toggle reports the
 * account's own choice.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { PlaybackWebhooksToggle, WebhookSettings } from "./WebhookSettings";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let calls: { method: string; url: string; body?: unknown }[];
let hooks: unknown[];

const EVENTS = ["playback.started", "playback.stopped", "scan.finished", "backup.taken"];

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  calls = [];
  hooks = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      const body = init?.body ? JSON.parse(String(init.body)) : undefined;
      calls.push({ method, url: String(url), body });
      const json = (v: unknown, status = 200) =>
        new Response(JSON.stringify(v), { status, headers: { "Content-Type": "application/json" } });
      if (method === "POST" && String(url) === "/api/webhooks") {
        const h = { id: "wh_1", url: body.url, events: body.events, enabled: true, created_at: 1, last: null };
        hooks.push(h);
        return json({ webhook: h, secret: "whsec_ONCE" }, 201);
      }
      if (method === "DELETE") {
        hooks = [];
        return new Response(null, { status: 204 });
      }
      return json({ webhooks: hooks, events: EVENTS, dropped: 0 });
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

async function render(node: React.ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(<QueryClientProvider client={client}>{node}</QueryClientProvider>);
  });
  await settle();
}

const button = (label: string) =>
  [...host.querySelectorAll("button")].find((b) => b.textContent === label);

function typeInto(el: HTMLInputElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
  act(() => {
    setter.call(el, value);
    el.dispatchEvent(new Event("input", { bubbles: true }));
  });
}

function check(label: string) {
  const box = [...host.querySelectorAll("label")]
    .find((l) => l.textContent?.includes(label))!
    .querySelector("input")!;
  act(() => {
    box.click();
  });
}

describe("webhooks pane", () => {
  it("says nothing is sent until one is added", async () => {
    await render(<WebhookSettings />);
    expect(host.textContent).toContain("Nothing is sent anywhere until you add one");
    expect(button("Add")!.disabled).toBe(true);
  });

  it("adds one with the events picked, and shows the secret once", async () => {
    await render(<WebhookSettings />);
    typeInto(host.querySelector('input[type="url"]') as HTMLInputElement, "http://ha.local:8123/api/webhook/x");
    check("A library scan finishes");
    expect(button("Add")!.disabled).toBe(false);
    await act(async () => {
      button("Add")!.closest("form")!.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    });
    await settle();
    const post = calls.find((c) => c.method === "POST")!;
    expect(post.body).toEqual({ url: "http://ha.local:8123/api/webhook/x", events: ["scan.finished"] });
    expect(host.textContent).toContain("whsec_ONCE");
    expect(host.textContent).toContain("isn’t shown again");
    // The list re-read shows the new webhook.
    expect(host.textContent).toContain("http://ha.local:8123/api/webhook/x");
  });

  it("says when a webhook has been failing, and asks before removing", async () => {
    hooks = [
      {
        id: "wh_1",
        url: "http://ha.local/",
        events: ["backup.taken"],
        enabled: true,
        created_at: 1,
        last: { at: 1793030000, event: "backup.taken", ok: false, status: 500, error: "the receiver did not answer with a 2xx status (500)", tries: 3, took_ms: 12000 },
      },
    ];
    await render(<WebhookSettings />);
    expect(host.querySelector("strong")?.textContent).toContain("failed after 3 tries");
    await act(async () => {
      button("Remove")!.click();
    });
    expect(calls.some((c) => c.method === "DELETE")).toBe(false);
    await act(async () => {
      button("Remove for good?")!.click();
    });
    await settle();
    expect(calls.some((c) => c.method === "DELETE" && c.url === "/api/webhooks/wh_1")).toBe(true);
  });
});

describe("the consent toggle", () => {
  it("shows the account's own value and reports a change", async () => {
    const changed: boolean[] = [];
    await render(<PlaybackWebhooksToggle on={false} onChange={(v) => changed.push(v)} />);
    const box = host.querySelector("input") as HTMLInputElement;
    expect(box.checked).toBe(false);
    expect(host.textContent).toContain("nobody else can turn it on for you");
    act(() => box.click());
    expect(changed).toEqual([true]);
  });
});
