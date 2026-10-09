/*
 * A room on somebody else's server, from this household's side
 * (federation Phase 5, step 5).
 *
 * Three things a person does: ask to join from People, be let in, and follow
 * the host's film. jsdom decodes nothing, so none of this proves a film plays
 * in step. What it proves is where every request went and what the screen
 * says, which is the part that can be wrong silently: a playback URL that
 * forgot it was in a room is refused by the host for a film that was never
 * shared, and looks exactly like their machine being off.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Routes, Route, useLocation } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PeerPlayer } from "./PeerPlayer";
import { People } from "./People";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const FP = "AAAABBBBCCCC";
const ROOM = "r1";
let host: HTMLDivElement;
let root: Root;
let calls: { method: string; url: string; body?: unknown }[] = [];

type Answer = { status: number; body?: unknown };

function room(positionMS: number, paused = false) {
  return {
    id: ROOM,
    item_id: 42,
    host_id: "u_chris",
    position_ms: positionMS,
    paused,
    updated_at: 0,
    age_ms: 0,
    created_at: 0,
    members: [
      { user_id: "u_chris", name: "Chris", host: true, last_seen: 0 },
      { user_id: "peer:X/local", name: "Georgia", host: false, last_seen: 0, peer: "X", server: "Utopia" },
    ],
  };
}

/*
 * One fake server for every route these screens touch. `route` answers the
 * together routes; everything else is the ordinary peer player's world.
 */
function mockServer(opts: {
  method?: string;
  positionMs?: number;
  route?: (method: string, path: string) => Answer | undefined;
  peers?: unknown;
}) {
  calls = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      calls.push({ method, url, body: init?.body ? JSON.parse(String(init.body)) : undefined });
      const json = (v: unknown, status = 200) =>
        new Response(JSON.stringify(v), { status, headers: { "Content-Type": "application/json" } });
      const path = url.split("?")[0];

      if (path.includes("/together")) {
        const a = opts.route?.(method, path);
        if (a) return a.status === 204 ? new Response(null, { status: 204 }) : json(a.body ?? {}, a.status);
        return json({ error: { code: "not_found", message: "no" } }, 404);
      }
      if (path.endsWith("/playback")) return json({ decision: { method: opts.method ?? "transcode", reason: "" } });
      if (path.endsWith("/subtitles")) return json({ subtitles: [] });
      if (/\/item\/\d+$/.test(path)) {
        const info: Record<string, unknown> = { title: "Blade Runner", duration_ms: 7_200_000 };
        if (opts.positionMs) info.position_ms = opts.positionMs;
        return json(info);
      }
      if (path.endsWith("/libraries")) return json({ libraries: [] });
      if (path === "/api/people/peers") return json({ peers: opts.peers ?? [] });
      if (path === "/api/people") return json({ people: [] });
      if (init?.method === "PUT") return new Response(null, { status: 204 });
      return json({});
    }),
  );
}

let landed = "";
function Probe() {
  const loc = useLocation();
  landed = loc.pathname + loc.search;
  return null;
}

async function render(at: string, settle = 25) {
  landed = "";
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, refetchInterval: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <FocusProvider>
          <MemoryRouter initialEntries={[at]}>
            <Probe />
            <Routes>
              <Route path="/people" element={<People />} />
              <Route path="/peers/:fingerprint/item/:item" element={<PeerPlayer />} />
            </Routes>
          </MemoryRouter>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await flush(settle);
}

async function flush(n = 10) {
  for (let i = 0; i < n; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
  }
}

async function waitFor(ok: () => boolean, ms = 4000) {
  const end = Date.now() + ms;
  while (!ok() && Date.now() < end) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 50));
    });
  }
}

beforeEach(() => {
  host = document.createElement("div");
  document.body.appendChild(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

describe("following a room on their server", () => {
  const joined = (r: ReturnType<typeof room>) => (method: string, path: string): Answer | undefined => {
    if (path === `/api/peers/${FP}/together/${ROOM}/join` && method === "POST") return { status: 200, body: r };
    if (path === `/api/peers/${FP}/together/${ROOM}` && method === "GET") return { status: 200, body: r };
    return undefined;
  };

  it("joins the room it arrived for", async () => {
    mockServer({ route: joined(room(0, true)) });
    await render(`/peers/${FP}/item/42?room=${ROOM}`);
    expect(calls.some((c) => c.method === "POST" && c.url === `/api/peers/${FP}/together/${ROOM}/join`)).toBe(true);
  });

  /*
   * The failure this exists for: one URL that forgot. The film was not shared,
   * so the host refuses it to anybody not named as a room member.
   */
  it("says it is a room member on every playback request", async () => {
    mockServer({ route: joined(room(0, true)) });
    await render(`/peers/${FP}/item/42?room=${ROOM}`);
    const el = host.querySelector("video")!;
    expect(el.getAttribute("src")).toContain("together=1");
    for (const part of ["/playback", "/subtitles", "/item/42"]) {
      const c = calls.find((x) => x.url.split("?")[0].endsWith(part));
      expect(c, `no request for ${part}`).toBeTruthy();
      expect(c!.url, part).toContain("together=1");
    }
  });

  it("follows the host to where the film is", async () => {
    mockServer({ route: joined(room(2_400_000)) });
    await render(`/peers/${FP}/item/42?room=${ROOM}`);
    await waitFor(() => (host.querySelector("video")?.getAttribute("src") ?? "").includes("t=24"));
    const src = host.querySelector("video")!.getAttribute("src")!;
    // A converted stream is moved by asking again from the new position.
    expect(src).toMatch(/t=24\d\d/);
    expect(src).toContain("together=1");
  });

  // In a room the host's position is where the film is, not ours.
  it("does not resume from where this household left off", async () => {
    mockServer({ positionMs: 600_000, route: joined(room(0, true)) });
    await render(`/peers/${FP}/item/42?room=${ROOM}`);
    expect(host.querySelector("video")!.getAttribute("src")).not.toContain("t=600");
  });

  it("names who is driving", async () => {
    mockServer({ route: joined(room(0, true)) });
    await render(`/peers/${FP}/item/42?room=${ROOM}`);
    await waitFor(() => (host.textContent ?? "").includes("Watching with Chris"));
    expect(host.textContent).toContain("Watching with Chris. They control playback.");
  });

  it("says when the room has ended", async () => {
    mockServer({ route: () => undefined }); // every together call is a 404
    await render(`/peers/${FP}/item/42?room=${ROOM}`);
    await waitFor(() => (host.textContent ?? "").includes("ended"));
    expect(host.textContent).toContain("This session has ended.");
  });

  // Outside a room nothing changes: no join, and nobody named.
  it("is the ordinary player without a room", async () => {
    mockServer({});
    await render(`/peers/${FP}/item/42`);
    expect(calls.some((c) => c.url.includes("/together"))).toBe(false);
    expect(calls.some((c) => c.url.includes("together=1"))).toBe(false);
    expect(host.querySelector("video")!.getAttribute("src")).not.toContain("together");
  });
});

describe("asking to join from People", () => {
  const peers = (watching?: string, shares = true) => [
    {
      fingerprint: FP,
      name: "Chris's",
      state: "paired",
      reachable: true,
      people: [{ id: "u_chris", name: "Chris", granted: true, shares, online: true, watching }],
    },
  ];
  const button = () =>
    [...host.querySelectorAll("button")].find((b) => b.textContent === "Ask to join");

  it("is offered beside somebody watching who lets you see them", async () => {
    mockServer({ peers: peers("Blade Runner") });
    await render("/people");
    expect(button()).toBeTruthy();
  });

  it("is not offered beside somebody idle, or somebody not sharing", async () => {
    mockServer({ peers: peers(undefined) });
    await render("/people");
    expect(button()).toBeFalsy();

    act(() => root.unmount());
    root = createRoot(host);
    mockServer({ peers: peers("Blade Runner", false) });
    await render("/people");
    expect(button()).toBeFalsy();
  });

  it("asks for the person, waits for the answer, and goes into their film", async () => {
    let answered = false;
    mockServer({
      peers: peers("Blade Runner"),
      route: (method, path) => {
        if (method === "POST" && path === `/api/peers/${FP}/together/requests`)
          return { status: 200, body: { id: "q1", state: "pending" } };
        if (method === "GET" && path === `/api/peers/${FP}/together/requests/q1`)
          return { status: 200, body: answered ? { id: "q1", state: "accepted", room_id: ROOM } : { id: "q1", state: "pending" } };
        if (path === `/api/peers/${FP}/together/${ROOM}/join`) return { status: 200, body: room(0, true) };
        if (path === `/api/peers/${FP}/together/${ROOM}`) return { status: 200, body: room(0, true) };
        return undefined;
      },
    });
    await render("/people");

    await act(async () => {
      button()!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    await flush();
    const ask = calls.find((c) => c.method === "POST" && c.url.endsWith("/together/requests"));
    expect(ask?.body).toEqual({ person: "u_chris" });
    expect(host.textContent).toContain("Asking Chris…");

    answered = true;
    await waitFor(() => landed.startsWith(`/peers/${FP}/item/42`), 5000);
    expect(landed).toBe(`/peers/${FP}/item/42?room=${ROOM}`);
  });

  it("can be cancelled while it waits, which tells their server and offers the button again", async () => {
    mockServer({
      peers: peers("Blade Runner"),
      route: (method, path) => {
        if (method === "POST" && path === `/api/peers/${FP}/together/requests`)
          return { status: 200, body: { id: "q1", state: "pending" } };
        if (path === `/api/peers/${FP}/together/requests/q1`)
          return { status: 200, body: { id: "q1", state: method === "DELETE" ? "withdrawn" : "pending" } };
        return undefined;
      },
    });
    await render("/people");
    await act(async () => {
      button()!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    await flush();
    const cancel = [...host.querySelectorAll("button")].find((b) => b.textContent === "Cancel");
    expect(cancel).toBeTruthy();
    await act(async () => {
      cancel!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    await flush();
    expect(calls.some((c) => c.method === "DELETE" && c.url.endsWith(`/together/requests/q1`))).toBe(true);
    expect(host.textContent).not.toContain("Asking Chris…");
    expect(button()).toBeTruthy();
  });

  it("says not now, and nothing more", async () => {
    mockServer({
      peers: peers("Blade Runner"),
      route: (method, path) =>
        method === "POST" && path === `/api/peers/${FP}/together/requests`
          ? { status: 200, body: { id: "", state: "not_now" } }
          : undefined,
    });
    await render("/people");
    await act(async () => {
      button()!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    await flush();
    expect(host.textContent).toContain("Not now");
    expect(landed).toBe("/people");
  });
});
