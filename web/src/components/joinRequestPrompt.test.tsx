/*
 * The host's answer to a friend asking to join (federation Phase 5, step 5).
 *
 * The consent the feature rests on is given here, in the moment, so the tests
 * are about it being asked at the right time and answered into the right room:
 * only while a film of yours plays, a yes opens a room around that film at
 * where you are and admits them to it, and a no says only no.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

// The player, as far as the prompt and the room can see it.
const player = { itemID: 5, isAudio: false, displayTime: 12, playing: true };
vi.mock("@/playback/PlaybackProvider", () => ({
  usePlayback: () => ({ ...player, seekTo: () => {}, togglePlay: () => {} }),
}));

import { TogetherProvider } from "@/playback/TogetherProvider";
import { JoinRequestPrompt } from "./JoinRequestPrompt";

let host: HTMLDivElement;
let root: Root;
let calls: { method: string; url: string; body?: unknown }[] = [];
let pending: unknown[] = [];

const request = {
  id: "q1",
  host_id: "u_chris",
  peer: "FP",
  person: "u_g",
  name: "Georgia",
  server: "Utopia",
  state: "pending",
  created_at: 1_000,
  expires_at: 1_060,
};

function session(members = 1) {
  return {
    id: "r1",
    item_id: 5,
    host_id: "u_chris",
    position_ms: 12_000,
    paused: false,
    updated_at: 0,
    age_ms: 0,
    created_at: 0,
    members: [
      { user_id: "u_chris", name: "Chris", host: true, last_seen: 0 },
      ...(members > 1
        ? [{ user_id: "peer:FP/u_g", name: "Georgia", host: false, last_seen: 0, peer: "FP", server: "Utopia" }]
        : []),
    ],
  };
}

function mockServer() {
  calls = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      calls.push({ method, url, body: init?.body ? JSON.parse(String(init.body)) : undefined });
      const json = (v: unknown, status = 200) =>
        new Response(JSON.stringify(v), { status, headers: { "Content-Type": "application/json" } });
      if (url === "/api/auth/status")
        return json({ authenticated: true, user: { id: "u_chris", name: "Chris", role: "admin" } });
      if (url === "/api/together/requests") return json({ requests: pending });
      if (url === "/api/together" && method === "POST") return json(session(), 201);
      if (url === "/api/together/requests/q1/accept") {
        pending = [];
        return json(session(2));
      }
      if (url === "/api/together/requests/q1/decline") {
        pending = [];
        return new Response(null, { status: 204 });
      }
      return json({});
    }),
  );
}

async function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <TogetherProvider>
          <JoinRequestPrompt />
        </TogetherProvider>
      </QueryClientProvider>,
    );
  });
  await flush();
}

async function flush(n = 15) {
  for (let i = 0; i < n; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
  }
}

function press(label: string) {
  const b = [...host.querySelectorAll("button")].find((x) => x.textContent === label);
  expect(b, `no "${label}" button`).toBeTruthy();
  return act(async () => {
    b!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  });
}

beforeEach(() => {
  host = document.createElement("div");
  document.body.appendChild(host);
  root = createRoot(host);
  Object.assign(player, { itemID: 5, isAudio: false, displayTime: 12, playing: true });
  pending = [request];
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

describe("a friend asking to join", () => {
  it("asks the host, by name and server, while their film plays", async () => {
    mockServer();
    await render();
    expect(host.textContent).toContain("Georgia on Utopia would like to watch this with you");
    // The server's own minute, counted from when this client saw it.
    const left = host.querySelector(".join-prompt__left")?.textContent;
    expect(["60s", "59s"]).toContain(left);
  });

  // Presence names nothing else, so there is nothing anybody could have asked
  // to join, and asking the server would be a request every few seconds for
  // a question with no possible answer.
  it("asks nothing while nothing of yours is playing, or only music is", async () => {
    for (const p of [{ itemID: 0 }, { isAudio: true }]) {
      Object.assign(player, { itemID: 5, isAudio: false }, p);
      mockServer();
      await render();
      expect(host.textContent).not.toContain("would like to watch");
      expect(calls.some((c) => c.url === "/api/together/requests")).toBe(false);
      act(() => root.unmount());
      root = createRoot(host);
    }
  });

  /*
   * Yes, with no room open: a room around this film at where the host is,
   * then the friend admitted to that room and no other.
   */
  it("opens a room around the film and lets them into it", async () => {
    mockServer();
    await render();
    await press("Let them join");
    await flush();

    const open = calls.find((c) => c.method === "POST" && c.url === "/api/together");
    expect(open?.body).toEqual({ item_id: 5, position_ms: 12_000 });
    const accept = calls.find((c) => c.url === "/api/together/requests/q1/accept");
    expect(accept?.body).toEqual({ room_id: "r1" });
    expect(host.textContent).not.toContain("would like to watch");
  });

  it("says not now and nothing else", async () => {
    mockServer();
    await render();
    await press("Not now");
    await flush();

    expect(calls.some((c) => c.method === "POST" && c.url === "/api/together/requests/q1/decline")).toBe(true);
    expect(calls.some((c) => c.url === "/api/together" && c.method === "POST")).toBe(false);
    expect(host.textContent).not.toContain("would like to watch");
  });
});

/*
 * The room outlives every screen now, so a host who stops, or moves to
 * another film, has to end it. Otherwise it goes on advertising a film nobody
 * is playing, and a friend admitted to it goes on being allowed that film.
 */
describe("a host's room after the film changes", () => {
  it("ends when the host stops playing", async () => {
    mockServer();
    await render();
    await press("Let them join");
    await flush();
    expect(calls.some((c) => c.method === "DELETE")).toBe(false);

    player.itemID = 0;
    await render();
    await flush();
    expect(calls.some((c) => c.method === "DELETE" && c.url === "/api/together/r1")).toBe(true);
  });
});
