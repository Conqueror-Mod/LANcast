/*
 * Sign-in with two-factor on (ADR 0100). A right password that answers with a
 * challenge leads to the code step; the code step sends the challenge with a
 * code or a recovery code; Start again goes back to the password.
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
let codeSteps: unknown[];
let codeAnswer: { status: number; body: unknown };

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  codeSteps = [];
  codeAnswer = { status: 200, body: { authenticated: true, user: { id: "u_1", name: "dana", role: "admin" } } };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const json = (v: unknown, status = 200) =>
        new Response(JSON.stringify(v), { status, headers: { "Content-Type": "application/json" } });
      if (url.includes("/api/auth/login/two-factor")) {
        codeSteps.push(JSON.parse(init!.body as string));
        return json(codeAnswer.body, codeAnswer.status);
      }
      if (url.includes("/api/auth/login")) {
        return json({ authenticated: false, two_factor: true, challenge: "ch-1" });
      }
      if (url.includes("/api/auth/accounts")) return json({ accounts: [] });
      if (url.includes("/api/auth/status")) return json({ configured: true, authenticated: false });
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

function type(input: HTMLInputElement, value: string) {
  const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
  act(() => {
    set.call(input, value);
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
}

const button = (text: string) =>
  [...host.querySelectorAll("button")].find((b) => b.textContent?.includes(text));

async function submit() {
  await act(async () => {
    host.querySelector("form")!.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
  });
  await settle();
}

async function passwordAccepted() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <Login />
      </QueryClientProvider>,
    );
  });
  await settle();
  type(host.querySelector('input[autocomplete="username"]') as HTMLInputElement, "dana");
  type(host.querySelector('input[type="password"]') as HTMLInputElement, "secret");
  await submit();
}

const codeInput = () => host.querySelector('input[autocomplete="one-time-code"]') as HTMLInputElement;

describe("signing in with two-factor on", () => {
  it("asks for a code after the right password, and sends it with the challenge", async () => {
    await passwordAccepted();
    expect(host.textContent).toContain("Enter your code");
    expect(host.querySelector('input[type="password"]')).toBeNull();

    type(codeInput(), "123 456");
    await submit();
    expect(codeSteps).toEqual([{ challenge: "ch-1", code: "123 456" }]);
  });

  it("takes a recovery code instead", async () => {
    await passwordAccepted();
    await act(async () => button("Use a recovery code")!.click());
    expect(host.textContent).toContain("Use a recovery code");
    type(codeInput(), "k7m2-9qxd");
    await submit();
    expect(codeSteps).toEqual([{ challenge: "ch-1", recovery_code: "k7m2-9qxd" }]);
  });

  it("will not send until the code is the right length", async () => {
    await passwordAccepted();
    type(codeInput(), "12345");
    const send = [...host.querySelectorAll('button[type="submit"]')][0] as HTMLButtonElement;
    expect(send.disabled).toBe(true);
  });

  it("says what the server said when the code is wrong, and clears it", async () => {
    codeAnswer = { status: 401, body: { error: { code: "unauthorized", message: "that code is not right" } } };
    await passwordAccepted();
    type(codeInput(), "000000");
    await submit();
    expect(host.querySelector('[role="alert"]')?.textContent).toContain("that code is not right");
    expect(codeInput().value).toBe("");
  });

  it("goes back to the password from Start again", async () => {
    await passwordAccepted();
    await act(async () => button("Start again")!.click());
    expect(host.textContent).not.toContain("Enter your code");
    expect(host.querySelector('input[type="password"]')).toBeTruthy();
    expect((host.querySelector('input[type="password"]') as HTMLInputElement).value).toBe("");
  });
});
