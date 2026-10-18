/*
 * The sign-in page's account picker. The server decides who sees names (home
 * network only); the page shows tiles when it is sent some and the ordinary
 * form when it is not, and signs in as whoever was picked.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Login } from "./Auth";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let logins: unknown[];

function mount(accounts: string[]) {
  logins = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const json = (v: unknown, status = 200) =>
        new Response(JSON.stringify(v), { status, headers: { "Content-Type": "application/json" } });
      if (url.includes("/api/auth/accounts")) return json({ accounts: accounts.map((name) => ({ name })) });
      if (url.includes("/api/auth/login")) {
        logins.push(JSON.parse(init!.body as string));
        return json({ error: { code: "unauthorized", message: "wrong" } }, 401);
      }
      if (url.includes("/api/auth/status")) return json({ configured: true, authenticated: false });
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

async function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <Login />
      </QueryClientProvider>,
    );
  });
  for (let i = 0; i < 4; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

function button(text: string) {
  return [...host.querySelectorAll("button")].find((b) => b.textContent?.includes(text));
}

function type(input: HTMLInputElement, value: string) {
  const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
  act(() => {
    set.call(input, value);
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
}

describe("the sign-in page's account picker", () => {
  it("offers the accounts it is sent, and signs in as the one picked", async () => {
    mount(["dana", "kids"]);
    await render();
    expect(host.textContent).toContain("Who's signing in?");
    expect(host.querySelector('input[type="password"]')).toBeNull();

    await act(async () => button("kids")!.click());
    expect(host.textContent).toContain("Not you?");
    type(host.querySelector('input[type="password"]') as HTMLInputElement, "secret");
    await act(async () => {
      host.querySelector("form")!.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    });
    expect(logins).toEqual([{ username: "kids", password: "secret" }]);
  });

  it("goes back to the names from Not you, and to the form from Someone else", async () => {
    mount(["dana", "kids"]);
    await render();
    await act(async () => button("dana")!.click());
    await act(async () => button("Not you?")!.click());
    expect(host.textContent).toContain("Who's signing in?");
    await act(async () => button("Someone else")!.click());
    expect(host.querySelector('input[autocomplete="username"]:not([type="hidden"])')).toBeTruthy();
  });

  // What a request from the internet is sent: nothing to pick from.
  it("is the ordinary form when it is sent no names", async () => {
    mount([]);
    await render();
    expect(host.textContent).not.toContain("Who's signing in?");
    expect(host.querySelector('input[autocomplete="username"]')).toBeTruthy();
    expect(host.querySelector('input[type="password"]')).toBeTruthy();
  });
});
