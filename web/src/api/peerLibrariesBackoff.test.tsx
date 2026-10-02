/*
 * The rail stops hammering a peer that is switched off.
 *
 * Measured on a real install: one call a minute to a peer that had been off
 * for days, from each open window, every one of them a three-second outbound
 * attempt and (until the server half of this fix) a log line. The hook now
 * waits five minutes between attempts while the last one failed, and goes
 * back to every minute as soon as one succeeds.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { peerLibrariesInterval, usePeerLibraries } from "./hooks";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

describe("peerLibrariesInterval", () => {
  it("is a minute while the peer answers and five while it does not", () => {
    expect(peerLibrariesInterval(false)).toBe(60_000);
    expect(peerLibrariesInterval(true)).toBe(300_000);
  });
});

describe("usePeerLibraries against a peer that is off", () => {
  let host: HTMLDivElement;
  let root: Root;
  let calls = 0;
  let peerUp = false;

  function Rail() {
    usePeerLibraries("F7H2");
    return null;
  }

  beforeEach(() => {
    vi.useFakeTimers();
    calls = 0;
    peerUp = false;
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        calls++;
        return peerUp
          ? new Response(JSON.stringify({ libraries: [] }), {
              status: 200,
              headers: { "Content-Type": "application/json" },
            })
          : new Response(
              JSON.stringify({ error: { code: "peer_unreachable", message: "Utopia is not answering" } }),
              { status: 502, headers: { "Content-Type": "application/json" } },
            );
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
    vi.useRealTimers();
  });

  async function minutes(n: number) {
    for (let i = 0; i < n * 6; i++) {
      await act(async () => {
        await vi.advanceTimersByTimeAsync(10_000);
      });
    }
  }

  it("asks every five minutes, not every minute, while it is down", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    await act(async () => {
      root.render(
        <QueryClientProvider client={qc}>
          <Rail />
        </QueryClientProvider>,
      );
    });
    await minutes(0.1);
    expect(calls).toBe(1); // the first look

    await minutes(4.5);
    // At a minute apart this would be 5 by now.
    expect(calls).toBe(1);

    await minutes(1);
    expect(calls).toBe(2); // the five-minute retry
  });

  it("goes back to every minute once the peer answers", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    await act(async () => {
      root.render(
        <QueryClientProvider client={qc}>
          <Rail />
        </QueryClientProvider>,
      );
    });
    await minutes(0.1);
    peerUp = true;
    await minutes(5.1); // the backed-off retry lands, and succeeds
    const afterRecovery = calls;
    await minutes(3);
    expect(calls - afterRecovery).toBeGreaterThanOrEqual(2);
  });
});
