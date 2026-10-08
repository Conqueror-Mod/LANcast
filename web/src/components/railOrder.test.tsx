/*
 * The order of the rail's places, and where its breaks fall.
 *
 * Add-ons, Live TV, Downloads and Games used to follow straight on from the
 * last library in the list — with a paired server, that server's films — so
 * they read as four more things on somebody else's machine. They are two
 * groups now, each behind a break: what you watch or play, then what you
 * manage and who is here.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { AppShell } from "./AppShell";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/api/health")) return json({ status: "ok", version: "1", api_version: 1 });
      if (url.includes("/api/activity")) return json({ active: false, tasks: [] });
      if (url.includes("/api/auth/status")) {
        return json({
          authenticated: true,
          configured: true,
          user: { id: "u1", name: "chris", role: "admin" },
        });
      }
      if (url.includes("/api/libraries")) {
        return json([{ id: 1, name: "Movies", kind: "movie", item_count: 1, media_count: 1 }]);
      }
      return json({ items: [], total: 0 });
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
        <FocusProvider>
          <MemoryRouter initialEntries={["/"]}>
            <AppShell>
              <div />
            </AppShell>
          </MemoryRouter>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  for (let i = 0; i < 6; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

const names = (el: Element | null) =>
  [...(el?.querySelectorAll(".app-shell__lib") ?? [])].map((a) => a.getAttribute("title"));

describe("the rail", () => {
  it("keeps places that are not libraries out of the library list", async () => {
    await render();
    const libs = host.querySelector(".app-shell__libs");
    // The Game Hub is the one exception, by design: it stands for the retro
    // libraries and PC Games, so it takes the games slot among the libraries
    // (docs/game-hub-plan.md). An admin is offered it even with no retro
    // library yet, because its own page is where adding one is explained.
    expect(names(libs)).toEqual(["Movies", "Game Hub"]);
  });

  it("puts Live TV first behind its own break, then Add-ons, Downloads and People", async () => {
    await render();
    const groups = [...host.querySelectorAll(".app-shell__group")];
    expect(groups.length).toBe(2);
    // Games appears only in the desktop window, when asked for.
    expect(names(groups[0])).toEqual(["Live TV"]);
    expect(names(groups[1])).toEqual(["Add-ons", "Downloads", "People"]);
  });
});
