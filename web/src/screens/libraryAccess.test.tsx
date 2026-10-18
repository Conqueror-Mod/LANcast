/*
 * Which libraries a member can see, in Users & access (ADR 0084).
 *
 * jsdom sees no layout; what it can prove is the wiring. The risks are a
 * switch that sends the wrong set (unticking one library must hide that one
 * and keep the rest as they were), a summary that does not change with it,
 * and switches offered for an administrator, whom the server never restricts.
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

let host: HTMLDivElement;
let root: Root;
let puts: { url: string; body: unknown }[];
let accessCalls: string[];

const LIBRARIES = [
  { id: 1, name: "Films", kind: "movie", path: "X:/Films", roots: [], created_at: 0, item_count: 3 },
  { id: 2, name: "Private", kind: "movie", path: "X:/Private", roots: [], created_at: 0, item_count: 1 },
  { id: 3, name: "Music", kind: "music", path: "X:/Music", roots: [], created_at: 0, item_count: 9 },
];

beforeEach(() => {
  puts = [];
  accessCalls = [];
  const hidden: Record<string, number[]> = { u_kid: [3] };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), { status: 200, headers: { "Content-Type": "application/json" } });
      const u = String(url);
      if (u.includes("/api/auth/status")) {
        return json({ authenticated: true, configured: true, user: { id: "u_me", name: "dana", role: "admin" } });
      }
      const m = u.match(/\/api\/users\/([^/]+)\/libraries/);
      if (m) {
        accessCalls.push(m[1]);
        if (init?.method === "PUT") {
          const body = JSON.parse(String(init.body));
          puts.push({ url: u, body });
          hidden[m[1]] = body.hidden;
        }
        return json({ hidden: hidden[m[1]] ?? [], applies: true });
      }
      if (u.endsWith("/api/users")) {
        return json({
          users: [
            { id: "u_me", name: "dana", role: "admin" },
            { id: "u_kid", name: "kid", role: "member" },
            { id: "u_co", name: "partner", role: "admin" },
          ],
        });
      }
      if (u.endsWith("/api/libraries")) return json(LIBRARIES);
      return json({});
    }),
  );
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
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
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
  const pane = [...host.querySelectorAll("button")].find((b) => b.textContent?.trim() === "Users & access");
  expect(pane).toBeTruthy();
  await act(async () => pane!.click());
  await settle();
}

const row = (name: string) =>
  [...host.querySelectorAll(".set-lib")].find(
    (r) => r.querySelector(".set-row__title")?.textContent?.startsWith(name),
  ) as HTMLElement | undefined;

const box = (scope: HTMLElement, label: string) =>
  [...scope.querySelectorAll("label")].find((l) => l.textContent === label)?.querySelector("input");

describe("which libraries a member can see", () => {
  it("says on the row what is hidden, without opening it", async () => {
    await render();
    expect(row("kid")?.textContent).toContain("Sees 2 of 3 libraries");
  });

  it("hides one more library and keeps the rest as they were", async () => {
    await render();
    const kid = row("kid")!;
    const open = [...kid.querySelectorAll("button")].find((b) => b.textContent === "Libraries");
    await act(async () => open!.click());
    expect(box(kid, "Music")?.checked).toBe(false);
    expect(box(kid, "Private")?.checked).toBe(true);
    await act(async () => box(kid, "Private")!.click());
    await settle();
    expect(puts).toHaveLength(1);
    expect((puts[0].body as { hidden: number[] }).hidden.sort()).toEqual([2, 3]);
    expect(row("kid")?.textContent).toContain("Sees 1 of 3 libraries");
  });

  it("offers no switches for an administrator, who is never restricted", async () => {
    await render();
    const admin = row("partner")!;
    expect([...admin.querySelectorAll("button")].some((b) => b.textContent === "Libraries")).toBe(false);
    expect(accessCalls).not.toContain("u_co");
    expect(accessCalls).not.toContain("u_me");
  });
});
