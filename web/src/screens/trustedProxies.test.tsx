/*
 * Trusted proxies, in Server › Network.
 *
 * What jsdom can prove is the wiring: one line per entry reaches the server as
 * a list, what the server normalised is what is shown afterwards, and a
 * refusal is the server's sentence on the page.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TrustedProxiesSection } from "./Settings";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let sent: unknown[];
let stored: string[];

function stubServer() {
  sent = [];
  stored = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const json = (v: unknown, status = 200) =>
        new Response(JSON.stringify(v), { status, headers: { "Content-Type": "application/json" } });
      if (String(url).includes("/api/settings")) {
        if (init?.method === "PUT") {
          const body = JSON.parse(String(init.body));
          sent.push(body);
          const list: string[] = body.trusted_proxies;
          if (list.some((e) => !/^[0-9a-f:.]+(\/\d+)?$/i.test(e))) {
            return json(
              {
                error: {
                  code: "bad_request",
                  message: 'trusted_proxies: "proxy.local": not an IP address or CIDR network',
                },
              },
              400,
            );
          }
          stored = list.map((e) => (e.includes("/") ? e : `${e}/32`));
          return new Response(null, { status: 204 });
        }
        return json({ trusted_proxies: stored });
      }
      return json({});
    }),
  );
}

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  stubServer();
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
        <TrustedProxiesSection />
      </QueryClientProvider>,
    );
  });
  await settle();
}

function textarea() {
  return host.querySelector("textarea") as HTMLTextAreaElement;
}

function type(value: string) {
  const el = textarea();
  const setter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")!.set!;
  act(() => {
    setter.call(el, value);
    el.dispatchEvent(new Event("input", { bubbles: true }));
  });
}

function save() {
  const btn = [...host.querySelectorAll("button")].find((b) => b.textContent === "Save")!;
  return btn;
}

describe("trusted proxies", () => {
  it("sends one entry per line, and shows what the server stored", async () => {
    await render();
    expect(save().disabled).toBe(true);
    type("127.0.0.1\n\n  10.0.0.0/24  ");
    expect(save().disabled).toBe(false);
    await act(async () => {
      save().closest("form")!.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    });
    await settle();
    expect(sent).toEqual([{ trusted_proxies: ["127.0.0.1", "10.0.0.0/24"] }]);
    // The server's normalised form, not what was typed.
    expect(textarea().value).toBe("127.0.0.1/32\n10.0.0.0/24");
    expect(save().disabled).toBe(true);
  });

  it("shows the server's refusal and keeps what was typed", async () => {
    await render();
    type("proxy.local");
    await act(async () => {
      save().closest("form")!.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    });
    await settle();
    expect(host.textContent).toContain("not an IP address or CIDR network");
    expect(textarea().value).toBe("proxy.local");
  });

  it("clearing the box sends an empty list", async () => {
    stored = [];
    await render();
    type("127.0.0.1");
    await act(async () => {
      save().closest("form")!.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    });
    await settle();
    type("");
    await act(async () => {
      save().closest("form")!.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    });
    await settle();
    expect(sent[sent.length - 1]).toEqual({ trusted_proxies: [] });
  });
});
