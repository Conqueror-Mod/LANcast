/*
 * Pair a device (ADR 0125).
 *
 * What the screen says and sends. The two that matter for security: the device
 * is named, with "this gives … your account", before anything is approved; and
 * approving sends the password while refusing does not need one.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { Pair } from "./Pair";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let calls: { url: string; method: string; body: string }[];

const pending = {
  code: "7KQ2-M9XD",
  name: "Living room",
  device: "Roku",
  address: "192.0.2.40",
  created_at: 1795000000,
  expires_at: 1795000600,
};

function mount(known: boolean) {
  calls = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      calls.push({ url, method, body: String(init?.body ?? "") });
      const json = (v: unknown, status = 200) =>
        new Response(JSON.stringify(v), {
          status,
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/api/devices/pair/approve")) {
        const b = JSON.parse(String(init?.body));
        return json({ ...pending, approved: b.approve });
      }
      if (url.includes("/api/devices/pair/")) {
        return known
          ? json(pending)
          : json(
              {
                error: {
                  code: "not_found",
                  message: "no device is waiting with that code",
                },
              },
              404,
            );
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

async function render(entry = "/pair") {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={[entry]}>
          <Pair />
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
  await settle();
}

const text = () => host.textContent ?? "";
const button = (label: string) =>
  [...host.querySelectorAll("button")].find((b) =>
    b.textContent?.includes(label),
  ) as HTMLButtonElement;

async function type(input: HTMLInputElement, value: string) {
  await act(async () => {
    Object.getOwnPropertyDescriptor(
      window.HTMLInputElement.prototype,
      "value",
    )!.set!.call(input, value);
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
}

async function lookUp(code: string) {
  await type(host.querySelector("input.pair__code") as HTMLInputElement, code);
  await act(async () => button("Continue").click());
  await settle();
}

describe("pairing a device", () => {
  it("names the device and what approving gives it before asking for anything", async () => {
    mount(true);
    await render();
    await lookUp("7kq2 m9xd");

    expect(calls[0].url).toContain("/api/devices/pair/7kq2%20m9xd");
    expect(text()).toContain("This gives “Living room” (Roku) your account");
    expect(text()).toContain("192.0.2.40");
    expect(button("Approve").disabled).toBe(true);
  });

  it("approves with the password", async () => {
    mount(true);
    await render();
    await lookUp("7KQ2-M9XD");
    const pw = host.querySelector(
      'input[type="password"]',
    ) as HTMLInputElement;
    await type(pw, "a long enough password");
    await act(async () => button("Approve").click());
    await settle();

    const sent = calls.find((c) => c.url.includes("/approve"));
    expect(sent && JSON.parse(sent.body)).toEqual({
      code: "7KQ2-M9XD",
      approve: true,
      current_password: "a long enough password",
    });
    expect(text()).toContain("is paired");
  });

  it("refuses without a password", async () => {
    mount(true);
    await render();
    await lookUp("7KQ2-M9XD");
    await act(async () => button("Refuse").click());
    await settle();

    const sent = calls.find((c) => c.url.includes("/approve"));
    expect(sent && JSON.parse(sent.body)).toEqual({
      code: "7KQ2-M9XD",
      approve: false,
    });
    expect(text()).toContain("was refused");
  });

  it("says so when no device is waiting with the code", async () => {
    mount(false);
    await render();
    await lookUp("AAAA-BBBB");
    expect(text()).toContain("no device is waiting with that code");
    expect(text()).not.toContain("your account");
  });

  it("takes the code from the address, for a TV that shows a link", async () => {
    mount(true);
    await render("/pair?code=7KQ2-M9XD");
    const input = host.querySelector("input.pair__code") as HTMLInputElement;
    expect(input.value).toBe("7KQ2-M9XD");
  });
});
