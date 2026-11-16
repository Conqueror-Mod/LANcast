/*
 * One failed poll used to end a Watch Together room for this window (R14-6).
 *
 * Any error from GET /api/together/{id} — a Wi-Fi blip, a 500, the poll racing
 * a server restart — set "That session has ended." and stopped. A follower
 * dropped out of a room that was still running; a host stopped reporting, so
 * the room went stale for everybody. Only 404 (the room is gone) and 403 (you
 * are no longer in it) end it now; anything else keeps polling and, after a
 * few in a row, says it is reconnecting.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useTogether, type TogetherControls } from "./together";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const room = {
  id: "r1",
  item_id: 5,
  host_id: "u_host",
  position_ms: 0,
  paused: false,
  updated_at: 0,
  age_ms: 0,
  created_at: 0,
  members: [],
};

let host: HTMLDivElement;
let root: Root;
let t: TogetherControls;
/** What the next polls answer with, in order; the last one repeats. */
let answers: number[];

function Grab() {
  t = useTogether("u_me");
  return null;
}

beforeEach(() => {
  vi.useFakeTimers();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  answers = [200];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const json = (status: number, v: unknown) =>
        new Response(JSON.stringify(v), { status, headers: { "Content-Type": "application/json" } });
      if (url === "/api/together/r1/join") return json(200, room);
      if (url === "/api/together/r1") {
        const status = answers.length > 1 ? answers.shift()! : answers[0];
        if (status === 0) throw new TypeError("Failed to fetch");
        return status === 200 ? json(200, room) : json(status, { error: { code: "x", message: "x" } });
      }
      return json(200, {});
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

async function joined() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <Grab />
      </QueryClientProvider>,
    );
  });
  await act(async () => {
    await t.join("r1");
  });
  expect(t.session?.id).toBe("r1");
}

async function polls(n: number) {
  for (let i = 0; i < n; i++) {
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2000);
    });
  }
}

describe("a Watch Together room's poll", () => {
  it("stays in the room through a failed poll", async () => {
    await joined();
    answers = [500, 200];
    await polls(1);
    expect(t.session?.id).toBe("r1");
    expect(t.error).toBeNull();
    expect(t.reconnecting).toBe(false);
  });

  it("stays in the room when the network drops for a moment", async () => {
    await joined();
    answers = [0, 200];
    await polls(2);
    expect(t.session?.id).toBe("r1");
    expect(t.error).toBeNull();
  });

  it("says it is reconnecting after a few failures in a row, and stops saying so", async () => {
    await joined();
    answers = [503, 503, 503, 200];
    await polls(3);
    expect(t.session?.id).toBe("r1");
    expect(t.reconnecting).toBe(true);
    await polls(1);
    expect(t.reconnecting).toBe(false);
  });

  it("ends when the room is gone", async () => {
    await joined();
    answers = [404];
    await polls(1);
    expect(t.session).toBeNull();
    expect(t.error).toBe("That session has ended.");
  });

  it("ends when this person is no longer in it", async () => {
    await joined();
    answers = [403];
    await polls(1);
    expect(t.session).toBeNull();
    expect(t.error).toBe("That session has ended.");
  });
});
