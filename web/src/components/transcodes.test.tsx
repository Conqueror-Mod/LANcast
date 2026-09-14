/*
 * Conversions, in Settings > Activity.
 *
 * The panel exists because a film refused to play and the only place the reason
 * was written down was lancastd.log. So the assertions here are about the two
 * things that would have shortened that morning: that a slot held for nobody is
 * legible as such, and that Stop actually asks the server to end that session
 * and then refetches the list it was read from.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Transcodes } from "./Transcodes";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let asked: string[];

const abandoned = {
  id: "t-abandoned", item_id: 41, title: "Scream", owner: "chris",
  live: false, output: "hls", encoding: true, start_at: 0,
  idle_seconds: 480, running_seconds: 500, served_bytes: 0, finished: false,
};

const watching = {
  id: "t-watching", item_id: 88, title: "Spider-Verse", owner: "chris",
  live: false, output: "hls", encoding: true, start_at: 0,
  idle_seconds: 2, running_seconds: 300, served_bytes: 41 * 1048576,
  finished: false,
};

function stub(sessions: unknown[] = [abandoned, watching], max = 3) {
  asked = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      asked.push(method + " " + String(url));
      if (method !== "GET") return new Response(null, { status: 204 });
      return new Response(JSON.stringify({ max, sessions }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }),
  );
}

async function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <Transcodes />
      </QueryClientProvider>,
    );
  });
  await settle();
}

// React Query notifies on a macrotask, so a resolved promise is not enough.
async function settle() {
  for (let i = 0; i < 4; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
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

const text = () => host.textContent ?? "";
const buttons = () => [...host.querySelectorAll("button")];

describe("conversions panel", () => {
  it("says how many slots there are, not just how many are used", async () => {
    stub();
    await render();
    // "Two of three" is information; "two" is a number.
    expect(text()).toContain("2 of 3 slots in use");
  });

  it("names a session that has never served a byte", async () => {
    stub();
    await render();
    /*
     * The field the panel exists for. A row reading "nothing served" next to
     * eight minutes idle is the whole diagnosis, and it must not be shown as
     * "0 bytes" beside a real figure where the eye slides past it.
     */
    expect(text()).toContain("nothing served");
    expect(text()).toContain("41 MB");
  });

  it("stops the session it was asked to stop, and refetches", async () => {
    stub();
    await render();
    const rows = [...host.querySelectorAll(".trans__row")];
    // Idle-first ordering is the server's job; the first row is the wasted one.
    expect(rows[0].textContent).toContain("Scream");

    const stop = rows[0].querySelector("button")!;
    await act(async () => {
      stop.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    await settle();

    expect(asked).toContain("DELETE /api/transcodes/t-abandoned");
    // The invalidation rule: the list this was stopped from must be refetched,
    // or the row stays on screen and the server is quietly right on its own.
    const after = asked.indexOf("DELETE /api/transcodes/t-abandoned");
    expect(asked.slice(after + 1).some((a) => a.startsWith("GET /api/transcodes"))).toBe(true);
  });

  it("does not ask what a live channel is called", async () => {
    // A channel id is negated; there is no item behind it and no title to show.
    stub([{ ...abandoned, id: "t-live", item_id: -30598, title: undefined, live: true }]);
    await render();
    expect(text()).toContain("Live channel");
  });

  it("is honest about an idle server", async () => {
    stub([], 3);
    await render();
    expect(text()).toContain("Nothing is being converted");
    expect(buttons().length).toBe(0);
  });
});
