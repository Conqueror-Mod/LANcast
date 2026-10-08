/*
 * Joining a room on this server (the half of Watch Together that had an
 * endpoint and a hook, and nothing that called them).
 *
 * What can be wrong here without anything failing: offering a room you are
 * already in or host, offering a film your rating ceiling hides, and a Join
 * that joins without opening the film, which would leave you in a room
 * watching nothing.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, useLocation } from "react-router-dom";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const player = { itemID: 5, isAudio: false, displayTime: 0, playing: true };
vi.mock("@/playback/PlaybackProvider", () => ({
  usePlayback: () => ({ ...player, seekTo: () => {}, togglePlay: () => {} }),
}));

import { TogetherProvider } from "@/playback/TogetherProvider";
import { OpenSessions } from "./OpenSessions";

let host: HTMLDivElement;
let root: Root;
let calls: { method: string; url: string }[] = [];
let landed = "";

function room(id: string, item: number, hostID: string, hostName: string, others: string[] = []) {
  return {
    id,
    item_id: item,
    host_id: hostID,
    position_ms: 0,
    paused: false,
    updated_at: 0,
    age_ms: 0,
    created_at: 0,
    members: [
      { user_id: hostID, name: hostName, host: true, last_seen: 0 },
      ...others.map((o) => ({ user_id: o, name: o, host: false, last_seen: 0 })),
    ],
  };
}

function mockServer(sessions: unknown[], hidden: number[] = []) {
  calls = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      calls.push({ method, url });
      const json = (v: unknown, status = 200) =>
        new Response(JSON.stringify(v), { status, headers: { "Content-Type": "application/json" } });
      if (url === "/api/auth/status")
        return json({ authenticated: true, user: { id: "u_me", name: "Me", role: "member" } });
      if (url === "/api/together" && method === "GET") return json({ sessions });
      const item = url.match(/^\/api\/items\/(\d+)$/);
      if (item) {
        const id = Number(item[1]);
        if (hidden.includes(id)) return json({ error: { code: "not_found", message: "no" } }, 404);
        return json({ id, title: id === 7 ? "Arrival" : "Blade Runner", series: null });
      }
      if (url === "/api/together/r1/join") return json(room("r1", 7, "u_chris", "Chris", ["u_me"]));
      return json({});
    }),
  );
}

function Probe() {
  landed = useLocation().pathname;
  return null;
}

async function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={["/people"]}>
          <Probe />
          <TogetherProvider>
            <OpenSessions heading="Watching together now" />
          </TogetherProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
  for (let i = 0; i < 20; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
  }
}

beforeEach(() => {
  host = document.createElement("div");
  document.body.appendChild(host);
  root = createRoot(host);
  player.itemID = 5;
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

describe("the rooms you could join", () => {
  it("names who is watching what", async () => {
    mockServer([room("r1", 7, "u_chris", "Chris")]);
    await render();
    expect(host.textContent).toContain("Chris is watching Arrival");
    expect([...host.querySelectorAll("button")].some((b) => b.textContent === "Join")).toBe(true);
  });

  it("leaves out rooms you host or are already in", async () => {
    mockServer([room("r2", 7, "u_me", "Me"), room("r3", 8, "u_chris", "Chris", ["u_me"])]);
    await render();
    expect(host.textContent).not.toContain("is watching");
  });

  // A limited profile is never offered a film above its ceiling by way of
  // somebody else's evening: the item route refuses it, so the room is hidden.
  it("hides a room whose film you could not open", async () => {
    mockServer([room("r1", 7, "u_chris", "Chris")], [7]);
    await render();
    expect(host.textContent).not.toContain("is watching");
  });

  // Joining without opening the film would leave you in a room watching
  // nothing; the film is opened, then the room joined.
  it("opens the room's film and joins it", async () => {
    mockServer([room("r1", 7, "u_chris", "Chris")]);
    await render();
    const join = [...host.querySelectorAll("button")].find((b) => b.textContent === "Join")!;
    await act(async () => {
      join.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    for (let i = 0; i < 10; i++) {
      await act(async () => {
        await new Promise((r) => setTimeout(r, 0));
      });
    }
    expect(landed).toBe("/watch/7");
    expect(calls.some((c) => c.method === "POST" && c.url === "/api/together/r1/join")).toBe(true);
  });

  it("offers nothing when you are not in a player's reach", async () => {
    mockServer([room("r1", 7, "u_chris", "Chris")]);
    const qc = new QueryClient();
    await act(async () => {
      root.render(
        <QueryClientProvider client={qc}>
          <MemoryRouter>
            <OpenSessions />
          </MemoryRouter>
        </QueryClientProvider>,
      );
    });
    expect(host.textContent).toBe("");
    expect(calls.some((c) => c.url === "/api/together")).toBe(false);
  });
});
