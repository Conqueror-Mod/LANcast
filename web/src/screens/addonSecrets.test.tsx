/*
 * A granted secret with no value, on the Add-ons pane.
 *
 * The bug this covers was not in any of the code it touches. The server
 * resolved secrets from a fixed list of its own provider keys and answered
 * empty for every other name, so a third-party add-on could be granted a
 * secret, be listed as granted, and read nothing for ever — while the approval
 * dialog told the operator it would read a key it never could.
 *
 * Everything about that failure was invisible: the request succeeded, the grant
 * was real, the screen was right about the grant. What was missing was any
 * distinction between *granted* and *configured*, so that is what these assert.
 *
 * jsdom performs no layout, so this is about what the screen says and what it
 * sends — which is exactly where this failure lived.
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

type Plugin = {
  name: string;
  version: string;
  kind: string;
  signer: string;
  enabled: boolean;
  digest: string;
  requested: { http: string[]; secrets: string[] };
  granted: { http: string[]; secrets: string[] };
  secrets_configured: string[];
};

let host: HTMLDivElement;
let root: Root;
let sent: { url: string; method: string; body: string }[];
let plugins: Plugin[];

function plugin(configured: string[]): Plugin {
  return {
    name: "example",
    version: "0.1.0",
    kind: "rating_source",
    signer: "unsigned",
    enabled: true,
    digest: "abc123",
    requested: { http: ["api.example.com"], secrets: ["example_key"] },
    granted: { http: ["api.example.com"], secrets: ["example_key"] },
    secrets_configured: configured,
  };
}

function mount(initial: Plugin[]) {
  plugins = initial;
  sent = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });

      if (url.includes("/api/auth/status")) {
        return json({
          authenticated: true,
          configured: true,
          user: { id: "u1", name: "chris", role: "admin" },
        });
      }
      if (url.includes("/api/plugins")) {
        if (method !== "GET") {
          sent.push({ url, method, body: String(init?.body ?? "") });
          // The server stores it; the next listing says so. Modelling that is
          // the point — a screen that only updates its own local state would
          // pass a test the real thing fails.
          if (url.includes("/secrets/")) {
            const value = JSON.parse(String(init?.body ?? "{}")).value ?? "";
            plugins = plugins.map((p) => ({
              ...p,
              secrets_configured: value === "" ? [] : ["example_key"],
            }));
          }
          return new Response(null, { status: 204 });
        }
        return json({ plugins });
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
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
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
  const addons = [...host.querySelectorAll("button")].find(
    (b) => b.textContent?.trim() === "Add-ons",
  );
  if (addons) {
    await act(async () => addons.click());
    await settle();
  }
}

const text = () => host.textContent ?? "";

const button = (label: string) =>
  [...host.querySelectorAll("button")].find(
    (b) => b.textContent?.trim() === label,
  );

describe("a granted add-on secret", () => {
  it("says it needs a value, rather than only that it was granted", async () => {
    mount([plugin([])]);
    await render();

    expect(text()).toContain("example_key");
    expect(text()).toContain("Needs a value");
    // And on the add-on's own row, so it is visible without looking closer.
    expect(text()).toContain("1 key needs a value");
  });

  it("says it is set once it has a value, and offers no way to read it", async () => {
    mount([plugin(["example_key"])]);
    await render();

    expect(text()).toContain("Set.");
    expect(text()).not.toContain("Needs a value");
    expect(text()).not.toContain("key needs a value");
    // Nothing on the screen can show a value: the server never sends one.
    const inputs = [...host.querySelectorAll("input")];
    const secretField = inputs.find((i) => i.type === "password");
    expect(secretField).toBeTruthy();
    expect(secretField!.value).toBe("");
  });

  it("sends the value to the right endpoint, and refreshes what the row says", async () => {
    mount([plugin([])]);
    await render();

    const field = [...host.querySelectorAll("input")].find(
      (i) => i.type === "password",
    )!;
    await act(async () => {
      const setter = Object.getOwnPropertyDescriptor(
        window.HTMLInputElement.prototype,
        "value",
      )!.set!;
      setter.call(field, "hunter2");
      field.dispatchEvent(new Event("input", { bubbles: true }));
    });
    await act(async () => button("Save")!.click());
    await settle();

    expect(sent).toHaveLength(1);
    expect(sent[0].method).toBe("PUT");
    expect(sent[0].url).toContain("/api/plugins/example/secrets/example_key");
    expect(JSON.parse(sent[0].body).value).toBe("hunter2");

    /*
     * The invalidation, which is the project's most-repeated bug: the request
     * succeeds, the server is right, and only the picture is stale. A row still
     * reading "needs a value" after you gave it one is exactly that failure.
     */
    expect(text()).toContain("Set.");
    expect(text()).not.toContain("Needs a value");
  });

  it("clears a value, and says so afterwards", async () => {
    mount([plugin(["example_key"])]);
    await render();

    await act(async () => button("Clear")!.click());
    await settle();

    expect(sent).toHaveLength(1);
    expect(JSON.parse(sent[0].body).value).toBe("");
    expect(text()).toContain("Needs a value");
  });

  it("shows nothing for an add-on granted no secrets", async () => {
    const p = plugin([]);
    p.granted = { http: ["api.example.com"], secrets: [] };
    mount([p]);
    await render();

    expect(text()).not.toContain("Needs a value");
    expect(host.querySelector(".addon-secrets")).toBeNull();
  });
});
