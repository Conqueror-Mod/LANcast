/*
 * Earning achievements while playing, switched on from the profile
 * (ADR 0112): the password goes once to its own route and is not shown
 * again; turning it off refetches the view, so the switch never reads stale.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RetroAchievementsCard } from "./RetroAchievementsCard";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let view: Record<string, unknown>;
let calls: { url: string; method: string; body?: string }[];
let refuse: boolean;

const base = {
  available: true,
  connected: true,
  user: "Player1",
  points: 10,
  softcore_points: 0,
  true_points: 10,
  recent: [],
  games: [],
  earning: false,
  unlocks_waiting: 0,
};

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  calls = [];
  refuse = false;
  view = { ...base };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      calls.push({ url, method, body: init?.body as string | undefined });
      const json = (v: unknown, status = 200) =>
        new Response(JSON.stringify(v), { status, headers: { "Content-Type": "application/json" } });
      if (url.endsWith("/play") && method === "PUT") {
        if (refuse) return json({ error: { code: "bad_request", message: "Invalid user/password combination." } }, 400);
        view = { ...view, earning: true };
        return json(view);
      }
      if (url.endsWith("/play") && method === "DELETE") {
        view = { ...view, earning: false, unlocks_waiting: 0 };
        return new Response(null, { status: 204 });
      }
      return json(view);
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
        <RetroAchievementsCard />
      </QueryClientProvider>,
    );
  });
  await settle();
}

function button(text: string) {
  return [...host.querySelectorAll("button")].find((b) => b.textContent === text)!;
}

async function enterPassword(pw: string) {
  await act(async () => button("Turn on").click());
  const input = host.querySelector<HTMLInputElement>('input[type="password"]')!;
  const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
  act(() => {
    set.call(input, pw);
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
  await act(async () => {
    input.form!.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
  });
  await settle();
}

describe("earning while playing", () => {
  it("turns on with the password, once, and does not show it again", async () => {
    await render();
    expect(host.textContent).toContain("Earning while playing");
    await enterPassword("hunter2");
    const put = calls.find((c) => c.url.endsWith("/api/profile/retroachievements/play") && c.method === "PUT");
    expect(JSON.parse(put!.body!)).toEqual({ password: "hunter2" });
    expect(host.textContent).toContain("On. Games in the LANcast desktop app unlock achievements");
    // Hardcore is built and hidden until RetroAchievements approves LANcast (ADR 0112 §5): the switch says why.
    expect(host.textContent).toContain("Hardcore isn't offered yet.");
    expect(host.querySelector('input[type="password"]')).toBeNull();
  });

  it("says why a password was refused, and keeps the form", async () => {
    refuse = true;
    await render();
    await enterPassword("wrong");
    expect(host.textContent).toContain("Invalid user/password combination.");
    expect(host.querySelector('input[type="password"]')).toBeTruthy();
  });

  it("counts unlocks still waiting, and turning off reads the view again", async () => {
    view = { ...base, earning: true, unlocks_waiting: 2 };
    await render();
    expect(host.textContent).toContain("2 unlocks are waiting to reach RetroAchievements.");
    await act(async () => button("Turn off").click());
    await settle();
    expect(calls.some((c) => c.url.endsWith("/play") && c.method === "DELETE")).toBe(true);
    expect(button("Turn on")).toBeTruthy();
    expect(host.textContent).not.toContain("waiting to reach");
  });
});
