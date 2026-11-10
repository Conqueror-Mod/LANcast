/*
 * Play on another of your own devices (ADR 0117), as the two windows see it.
 *
 * The player half: a window that offers itself reports what it plays, collects
 * a command when a report says one is waiting, and opens the player for a
 * play. A window that does not offer itself says nothing at all.
 *
 * The remote half: the panel lists your other devices and never this one, and
 * "Move here" sends the moment you are at and then stops here.
 *
 * What it cannot prove is that the server keeps one account's players from
 * another's; internal/api/remote_test.go does that against the real handler.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

// The player, as far as the host can see it.
const player = {
  itemID: 7,
  item: { id: 7 } as { id: number } | undefined,
  displayTime: 61,
  totalDuration: 7200,
  playing: true,
  loading: false,
  volume: 0.8,
};
const steered: string[] = [];
vi.mock("@/playback/PlaybackProvider", () => ({
  usePlayback: () => ({
    ...player,
    seekTo: (t: number) => steered.push(`seek ${t}`),
    togglePlay: () => steered.push("toggle"),
    stop: () => steered.push("stop"),
    playNext: () => steered.push("next"),
    playPrev: () => steered.push("prev"),
    changeVolume: (v: number) => steered.push(`volume ${v}`),
  }),
}));

import { RemotePlayerHost } from "./RemotePlayerHost";
import { RemotePanel } from "./RemotePanel";
import { resetRemoteDeviceForTests, setRemoteDevice, getRemoteDevice, setRemoteNotice } from "@/playback/remote";

let host: HTMLDivElement;
let root: Root;
let calls: { method: string; url: string; body?: Record<string, unknown> }[] = [];
let pending = 0;
let commands: unknown[] = [];
let players: unknown[] = [];

function mockServer() {
  calls = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      calls.push({ method, url, body: init?.body ? JSON.parse(String(init.body)) : undefined });
      const json = (v: unknown, status = 200) =>
        new Response(JSON.stringify(v), { status, headers: { "Content-Type": "application/json" } });
      if (url === "/api/remote/players") return json({ players });
      if (url.endsWith("/commands") && method === "GET") {
        const out = commands;
        commands = [];
        pending = 0;
        return json({ commands: out });
      }
      if (url.endsWith("/commands") && method === "POST") return new Response(null, { status: 202 });
      if (url.startsWith("/api/remote/players/") && method === "PUT")
        return json({ id: "self", pending, age_ms: 0 });
      if (url.startsWith("/api/remote/players/") && method === "DELETE")
        return new Response(null, { status: 204 });
      if (url.startsWith("/api/items/")) return json({ id: Number(url.split("/").pop()), title: "Arrival", kind: "movie" });
      return json({});
    }),
  );
}

function Where() {
  const loc = useLocation();
  return <output data-testid="where">{loc.pathname + JSON.stringify(loc.state ?? null)}</output>;
}

async function flush(n = 15) {
  for (let i = 0; i < n; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
  }
}

async function render(ui: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={["/"]}>
          <Routes>
            <Route path="*" element={<Where />} />
          </Routes>
          {ui}
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
  await flush();
}

function where() {
  return host.querySelector("[data-testid=where]")?.textContent ?? "";
}

function press(label: string) {
  const b = [...host.querySelectorAll("button")].find((x) => x.textContent === label);
  expect(b, `no "${label}" button`).toBeTruthy();
  return act(async () => {
    b!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  });
}

beforeEach(() => {
  localStorage.clear();
  resetRemoteDeviceForTests();
  act(() => setRemoteNotice(null));
  host = document.createElement("div");
  document.body.appendChild(host);
  root = createRoot(host);
  Object.assign(player, { itemID: 7, item: { id: 7 }, displayTime: 61, totalDuration: 7200, playing: true, loading: false });
  steered.length = 0;
  pending = 0;
  commands = [];
  players = [];
  mockServer();
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

describe("this window as a player", () => {
  it("says nothing while it does not offer itself", async () => {
    setRemoteDevice({ enabled: false });
    await render(<RemotePlayerHost />);
    expect(calls.filter((c) => c.url.startsWith("/api/remote"))).toEqual([]);
  });

  it("reports what it is playing", async () => {
    setRemoteDevice({ enabled: true, name: "Den TV" });
    await render(<RemotePlayerHost />);
    const put = calls.find((c) => c.method === "PUT");
    expect(put?.url).toBe(`/api/remote/players/${getRemoteDevice().id}`);
    expect(put?.body).toMatchObject({
      name: "Den TV",
      item_id: 7,
      position_ms: 61_000,
      duration_ms: 7_200_000,
      paused: false,
      volume: 0.8,
    });
  });

  /*
   * A report that says a command is waiting is enough on its own: the event
   * may have been missed, and the command must not be.
   */
  it("collects a waiting command and opens the player for a play", async () => {
    setRemoteDevice({ enabled: true });
    pending = 1;
    commands = [{ action: "play", item_id: 12, queue: [12, 13], from: "Phone", sent_at: 0 }];
    await render(<RemotePlayerHost />);
    expect(where()).toBe('/watch/12{"queue":[12,13],"fromStart":false,"continuing":false}');
    expect(host.textContent).toContain("Playing from Phone");
  });

  /*
   * "Move here" carries the moment. It must open as a continuation (no
   * pre-roll) and seek once the item is playing, not before.
   */
  it("a moved film continues and seeks to the moment it was sent at", async () => {
    setRemoteDevice({ enabled: true });
    pending = 1;
    commands = [{ action: "play", item_id: 12, position_ms: 2_400_000, sent_at: 0 }];
    await render(<RemotePlayerHost />);
    expect(where()).toContain('"continuing":true');
    expect(steered).not.toContain("seek 2400");

    // The player has opened the item and it plays; the seek follows.
    Object.assign(player, { itemID: 12, item: { id: 12 } });
    await render(<RemotePlayerHost />);
    await act(async () => {
      await new Promise((r) => setTimeout(r, 700));
    });
    expect(steered).toContain("seek 2400");
  });

  it("pauses what it is playing", async () => {
    setRemoteDevice({ enabled: true });
    pending = 1;
    commands = [{ action: "pause", sent_at: 0 }];
    await render(<RemotePlayerHost />);
    expect(steered).toEqual(["toggle"]);
  });
});

describe("the remote", () => {
  const tv = {
    id: "tv-0123456789",
    name: "Living room",
    kind: "desktop",
    item_id: 4,
    position_ms: 10_000,
    duration_ms: 60_000,
    paused: true,
    volume: 1,
    age_ms: 0,
    pending: 0,
  };

  it("lists your other devices and not this one", async () => {
    players = [tv, { ...tv, id: getRemoteDevice().id, name: "This one" }];
    await render(<RemotePanel />);
    expect(host.textContent).toContain("Living room");
    expect(host.textContent).not.toContain("This one");
  });

  it("says how to offer a device when there is none", async () => {
    players = [];
    await render(<RemotePanel />);
    expect(host.textContent).toContain("None of your other devices is offering itself");
  });

  it("Move here sends the moment you are at, then stops here", async () => {
    players = [tv];
    const after = vi.fn();
    await render(
      <RemotePanel
        send={{ label: "Move here", itemID: 7, queue: [7, 8], positionMS: 61_000, after }}
      />,
    );
    await press("Move here");
    await flush();
    const post = calls.find((c) => c.method === "POST");
    expect(post?.url).toBe("/api/remote/players/tv-0123456789/commands");
    expect(post?.body).toMatchObject({ action: "play", item_id: 7, queue: [7, 8], position_ms: 61_000 });
    // Named even though this window does not offer itself, so the screen it
    // sent to can say which device it was.
    expect(post?.body).toMatchObject({ from_name: getRemoteDevice().name });
    expect(post?.body?.from_player).toBeUndefined();
    expect(after).toHaveBeenCalled();
  });

  it("a paused device offers Play, and pressing it resumes there", async () => {
    players = [tv];
    await render(<RemotePanel />);
    const play = host.querySelector<HTMLButtonElement>('button[aria-label="Play"]');
    expect(play).toBeTruthy();
    await act(async () => {
      play!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    await flush();
    expect(calls.find((c) => c.method === "POST")?.body).toMatchObject({ action: "resume" });
  });

  it("does not stop here when the device refused", async () => {
    players = [tv];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        if (url === "/api/remote/players") return new Response(JSON.stringify({ players }), { status: 200 });
        if (init?.method === "POST")
          return new Response(JSON.stringify({ error: { code: "not_found", message: "that device is not available" } }), {
            status: 404,
          });
        return new Response(JSON.stringify({}), { status: 200 });
      }),
    );
    const after = vi.fn();
    await render(<RemotePanel send={{ label: "Move here", itemID: 7, queue: [7], positionMS: 0, after }} />);
    await press("Move here");
    await flush();
    expect(after).not.toHaveBeenCalled();
    expect(host.textContent).toContain("that device is not available");
  });
});
