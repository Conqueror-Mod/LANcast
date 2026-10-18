/*
 * Your RetroAchievements on the profile page (ADR 0090): a form until you
 * connect, then your progress; pictures are LANcast's URLs; a refused key
 * comes back as RetroAchievements' sentence.
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
let calls: { method: string; body?: string }[];
let putStatus: number;

const connected = {
  available: true,
  connected: true,
  user: "Player1",
  avatar: "/api/retroachievements/media/UserPic/Player1.png",
  points: 1200,
  softcore_points: 40,
  true_points: 3000,
  recent: [
    { achievement_id: 9, title: "First", description: "", points: 5, hardcore: true, date: "2026-10-17 21:00:00",
      badge: "/api/retroachievements/media/Badge/123.png", game_id: 3, game_title: "A Game", console: "SNES" },
  ],
  games: [{ game_id: 3, title: "A Game", console: "SNES", awarded: 5, awarded_hardcore: 4, max: 20, award: "beaten-hardcore" }],
};

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  calls = [];
  putStatus = 200;
  view = { available: true, connected: false, points: 0, softcore_points: 0, true_points: 0, recent: [], games: [] };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      calls.push({ method, body: init?.body as string | undefined });
      const json = (v: unknown, status = 200) =>
        new Response(JSON.stringify(v), { status, headers: { "Content-Type": "application/json" } });
      if (method === "PUT") {
        if (putStatus !== 200) {
          return json({ error: { code: "bad_request", message: "RetroAchievements did not accept that username and API key" } }, putStatus);
        }
        view = connected;
        return json(view);
      }
      if (method === "DELETE") {
        view = { ...view, connected: false };
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

async function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <RetroAchievementsCard />
      </QueryClientProvider>,
    );
  });
  for (let i = 0; i < 4; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

function type(label: string, value: string) {
  const input = [...host.querySelectorAll("label")].find((l) => l.textContent?.includes(label))!.querySelector("input")!;
  const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
  act(() => {
    set.call(input, value);
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
}

async function submit() {
  await act(async () => {
    host.querySelector("form")!.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
  });
  for (let i = 0; i < 4; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

describe("RetroAchievements on the profile", () => {
  it("offers to connect, and then shows the progress", async () => {
    await render();
    expect(host.textContent).toContain("Only you see it");
    type("Username", " Player1 ");
    type("Web API key", "the-key");
    await submit();
    const put = calls.find((c) => c.method === "PUT");
    expect(JSON.parse(put!.body!)).toEqual({ username: "Player1", api_key: "the-key" });
    expect(host.textContent).toContain("Player1");
    expect(host.textContent).toContain("1,200 points");
    expect(host.textContent).toContain("First");
    expect(host.textContent).toContain("5 of 20");
    // Every picture is LANcast's own URL.
    for (const img of host.querySelectorAll("img")) {
      expect(img.getAttribute("src")).toMatch(/^\/api\/retroachievements\/media\//);
    }
  });

  it("says why a key was refused, and keeps the form", async () => {
    putStatus = 400;
    await render();
    type("Username", "Player1");
    type("Web API key", "wrong");
    await submit();
    expect(host.textContent).toContain("did not accept that username and API key");
    expect(host.querySelector("form")).toBeTruthy();
  });

  it("disconnects", async () => {
    view = connected;
    await render();
    const button = [...host.querySelectorAll("button")].find((b) => b.textContent === "Disconnect")!;
    await act(async () => button.click());
    for (let i = 0; i < 4; i++) {
      await act(async () => {
        await new Promise((r) => setTimeout(r, 5));
      });
    }
    expect(calls.some((c) => c.method === "DELETE")).toBe(true);
    expect(host.querySelector("form")).toBeTruthy();
  });
});
