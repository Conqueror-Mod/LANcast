/*
 * A follower in a room on this server, on a converted stream (R14-4).
 *
 * A seek on a converted stream restarts the conversion, and until its first
 * frame arrives displayTime reads the seek target while the room moves on. The
 * local follower used the bare 1.5 s rule on every poll, so a conversion slower
 * than that to start was thrown away and restarted every two seconds. These
 * drive TogetherProvider's follower with polls and a clock, and count seeks.
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

// The player as the room sees it. displayTime is the converted stream's real
// position: its offset plus the element's clock.
const player = {
  itemID: 5,
  isAudio: false,
  displayTime: 0,
  playing: true,
  loading: false,
  converting: true,
  // The stream is a finished remux the seek target is inside (inPlaceSeek.ts).
  inPlace: false,
};
const seeks: number[] = [];
vi.mock("@/playback/PlaybackProvider", () => ({
  usePlayback: () => ({
    ...player,
    seekRestarts: () => player.converting && !player.inPlace,
    seekTo: (t: number) => {
      seeks.push(t);
      // A converted seek: the offset moves to the target and nothing plays
      // until the new conversion's first frame. One inside a finished remux
      // just moves the clock.
      player.displayTime = t;
      if (player.converting && !player.inPlace) player.loading = true;
    },
    togglePlay: () => {
      player.playing = !player.playing;
    },
  }),
}));

import { TogetherProvider, useTogetherRoom } from "./TogetherProvider";
import type { TogetherControls } from "./together";

let host: HTMLDivElement;
let root: Root;
let clock = 1_000_000;
let room: TogetherControls | null = null;

function session(positionMS: number) {
  return {
    id: "r1",
    item_id: 5,
    host_id: "u_host",
    position_ms: positionMS,
    paused: false,
    updated_at: 0,
    age_ms: 0,
    created_at: 0,
    members: [
      { user_id: "u_host", name: "Host", host: true, last_seen: 0 },
      { user_id: "u_me", name: "Me", host: false, last_seen: 0 },
    ],
  };
}

function Grab() {
  room = useTogetherRoom();
  return null;
}

beforeEach(() => {
  host = document.createElement("div");
  document.body.appendChild(host);
  root = createRoot(host);
  seeks.length = 0;
  clock = 1_000_000;
  Object.assign(player, { displayTime: 0, playing: true, loading: false, converting: true, inPlace: false });
  vi.spyOn(Date, "now").mockImplementation(() => clock);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), { status: 200, headers: { "Content-Type": "application/json" } });
      if (url === "/api/auth/status")
        return json({ authenticated: true, user: { id: "u_me", name: "Me", role: "user" } });
      if (url === "/api/together/r1/join") return json(session(100_000));
      if (url.startsWith("/api/together/requests")) return json({ requests: [] });
      // The room's own polls never land in these tests; adopt() stands in.
      return new Promise<Response>(() => {});
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

async function flush(n = 10) {
  for (let i = 0; i < n; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
  }
}

async function joinRoom() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <TogetherProvider>
          <Grab />
        </TogetherProvider>
      </QueryClientProvider>,
    );
  });
  await flush();
  await act(async () => {
    await room!.join("r1");
  });
  await flush();
}

// One poll: two seconds pass and the room answers with the host's position.
async function poll(hostMS: number) {
  clock += 2000;
  await act(async () => {
    room!.adopt(session(hostMS));
  });
}

describe("a converted follower in a room here", () => {
  it("does not seek again while the conversion is starting", async () => {
    await joinRoom();
    expect(seeks).toHaveLength(1); // joining at 0 s into a room at 100 s

    // Ten polls (twenty seconds) of a conversion that has not yet drawn a
    // frame: the clock reads the seek target the whole time.
    for (let i = 1; i <= 10; i++) await poll(100_000 + i * 2000);
    expect(seeks).toHaveLength(1);
  });

  it("never seeks while in step, judged by offset plus the element's time", async () => {
    await joinRoom();
    await poll(102_000); // still starting
    // The first conversion lands and plays in step with the room.
    player.loading = false;
    let hostMS = 104_000;
    player.displayTime = hostMS / 1000;
    await poll(hostMS);
    const after = seeks.length;
    for (let i = 0; i < 15; i++) {
      hostMS += 2000;
      // The offset (the seek target) plus the element's clock since it started.
      player.displayTime = seeks[0] + (hostMS - 104_000) / 1000;
      await poll(hostMS);
    }
    expect(seeks).toHaveLength(after);
  });

  it("still catches a follower that is far out once the picture is up", async () => {
    await joinRoom();
    await poll(102_000); // still starting
    player.loading = false;
    player.displayTime = 104;
    await poll(104_000);
    // Thirty seconds on, the follower is a minute behind.
    clock += 30_000;
    player.displayTime = 70;
    await poll(132_000);
    expect(seeks.length).toBeGreaterThan(1);
  });
});

/*
 * Ledger row 6 (2026-11-16): a browser guest on an AC3 episode, video copied
 * and audio converted. The first stream took ~25 s to start (the server
 * finishes the remux before serving the playlist) and landed about 20 s
 * behind; every catch-up seek then threw that finished remux away for another
 * 25 s start, four times in ninety seconds.
 */
describe("a follower inside a finished remux", () => {
  // Joins, waits out a 24-second start, and lands the picture at the seek
  // target while the host has moved on: the moment row 6 went wrong.
  async function landedBehind() {
    await joinRoom();
    expect(seeks).toHaveLength(1);
    const landedAt = seeks[0];
    for (let i = 1; i <= 12; i++) await poll(100_000 + i * 2000); // 24 s of start-up
    player.loading = false;
    player.displayTime = landedAt;
    player.inPlace = true; // the remux is finished and covers the rest
    return landedAt;
  }

  it("catches up with one seek to where the host is, without a lead", async () => {
    await landedBehind();
    // The first poll after the picture lands finds it about 20 s behind.
    await poll(126_000);
    expect(seeks).toHaveLength(2);
    // Where the host is, not ahead of it: an in-place seek lands at once.
    expect(seeks[1]).toBeCloseTo(126, 0);
    await poll(128_000);
    expect(seeks).toHaveLength(2);
  });

  it("stays put once caught up, as direct play would", async () => {
    await landedBehind();
    let hostMS = 126_000;
    await poll(hostMS);
    await poll((hostMS += 2000));
    const after = seeks.length;
    for (let i = 0; i < 15; i++) {
      hostMS += 2000;
      player.displayTime = hostMS / 1000 - 0.3;
      await poll(hostMS);
    }
    expect(seeks).toHaveLength(after);
  });

  it("is held to the direct-play tolerance, not the converted one", async () => {
    await landedBehind();
    let hostMS = 126_000;
    await poll(hostMS);
    await poll((hostMS += 2000));
    const after = seeks.length;
    // Four seconds behind: inside the 8 s converted tolerance, outside 1.5 s.
    hostMS += 6000;
    clock += 4000;
    player.displayTime = hostMS / 1000 - 4;
    await poll(hostMS);
    expect(seeks.length).toBe(after + 1);
  });
});
