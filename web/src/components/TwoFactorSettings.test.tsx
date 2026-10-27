/*
 * Two-factor sign-in in the Security pane (ADR 0100): turning it on shows the
 * key and then the recovery codes, once; turning it off asks for the password.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TwoFactorSettings } from "./TwoFactorSettings";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let state: { enabled: boolean; available: boolean; recovery_codes_left: number; enabled_at?: number };
let calls: { method: string; url: string; body?: unknown }[];

const CODES = ["aaaa-bbbb", "cccc-dddd"];

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  calls = [];
  state = { enabled: false, available: true, recovery_codes_left: 0 };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      const body = init?.body ? JSON.parse(init.body as string) : undefined;
      calls.push({ method, url, body });
      const json = (v: unknown, status = 200) =>
        new Response(JSON.stringify(v), { status, headers: { "Content-Type": "application/json" } });
      if (url.endsWith("/api/profile/two-factor/setup")) {
        return json({ key: "GEZD GNBV", url: "otpauth://totp/LANcast:dana?secret=GEZDGNBV" });
      }
      if (url.endsWith("/api/profile/two-factor") && method === "POST") {
        state = { enabled: true, available: true, recovery_codes_left: 2, enabled_at: 1_700_000_000 };
        return json({ recovery_codes: CODES });
      }
      if (url.endsWith("/api/profile/two-factor") && method === "DELETE") {
        if (body?.current_password !== "right") {
          return json({ error: { code: "forbidden", message: "that password is not right" } }, 403);
        }
        state = { enabled: false, available: true, recovery_codes_left: 0 };
        return new Response(null, { status: 204 });
      }
      if (url.endsWith("/api/profile/two-factor")) return json(state);
      return json({});
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

async function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <TwoFactorSettings />
      </QueryClientProvider>,
    );
  });
  await settle();
}

function type(input: HTMLInputElement, value: string) {
  const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
  act(() => {
    set.call(input, value);
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
}

const button = (text: string) =>
  [...host.querySelectorAll("button")].find((b) => b.textContent?.trim() === text);

describe("two-factor sign-in in Settings", () => {
  it("turns on with a code from the app, then shows the recovery codes once", async () => {
    await render();
    await act(async () => button("Turn on")!.click());
    await settle();
    expect(host.textContent).toContain("GEZD GNBV");
    expect(host.querySelector('a[href^="otpauth://"]')).toBeTruthy();

    type(host.querySelector('input[aria-label="Code from the app"]') as HTMLInputElement, "123456");
    await act(async () => {
      host.querySelector("form")!.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    });
    await settle();
    expect(calls.find((c) => c.method === "POST" && c.url.endsWith("/api/profile/two-factor"))?.body).toEqual({
      code: "123456",
    });
    for (const c of CODES) expect(host.textContent).toContain(c);

    await act(async () => button("I’ve saved them")!.click());
    await settle();
    for (const c of CODES) expect(host.textContent).not.toContain(c);
    expect(host.textContent).toContain("2 recovery codes left");
    expect(host.textContent).toContain("Running low");
  });

  it("asks for the password to turn it off, and says when it is wrong", async () => {
    state = { enabled: true, available: true, recovery_codes_left: 10, enabled_at: 1_700_000_000 };
    await render();
    await act(async () => button("Turn off")!.click());
    const pw = host.querySelector('input[type="password"]') as HTMLInputElement;
    type(pw, "wrong");
    await act(async () => {
      host.querySelector("form")!.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    });
    await settle();
    expect(host.querySelector('[role="alert"]')?.textContent).toContain("that password is not right");

    type(host.querySelector('input[type="password"]') as HTMLInputElement, "right");
    await act(async () => {
      host.querySelector("form")!.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    });
    await settle();
    expect(button("Turn on")).toBeTruthy();
  });

  it("says it is unavailable rather than offering a button that fails", async () => {
    state = { enabled: false, available: false, recovery_codes_left: 0 };
    await render();
    expect(button("Turn on")).toBeUndefined();
    expect(host.textContent).toContain("unavailable");
  });
});
