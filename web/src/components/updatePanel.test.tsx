/*
 * The update panel's download state.
 *
 * Reported from a real install: the panel sat on "Downloading…" while the
 * activity indicator, three inches away, already said the update was ready.
 * Nothing was wrong on the server — POST /api/update/download returns
 * immediately and downloads in the background, and the panel's status query was
 * cached for a minute with nothing to tell it to look again. Two surfaces
 * reading the same server and disagreeing, which reads as a hang.
 *
 * So what is asserted here is the *refetch*: that pressing Download makes the
 * panel keep asking, and that it stops asking once the answer arrives.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { UpdateSettings } from "./UpdateSettings";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let statusBody: Record<string, unknown>;
let statusReads: number;

const base = {
  supported: true,
  current: "0.6.13",
  latest: "v0.6.14",
  available: true,
  can_verify: true,
  enabled: true,
  checking: false,
  error: "",
  download_error: "",
};

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  statusBody = { ...base };
  statusReads = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if ((init?.method ?? "GET") !== "GET") return json({});
      if (url.includes("/api/update")) {
        statusReads++;
        return json(statusBody);
      }
      if (url.includes("/api/settings")) return json({ update_check: true });
      return json({});
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
        <MemoryRouter>
          <UpdateSettings />
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
  await settle();
}

async function settle(ms = 5) {
  await act(async () => {
    await new Promise((r) => setTimeout(r, ms));
  });
}

function button(text: RegExp): HTMLButtonElement {
  const b = [...host.querySelectorAll<HTMLButtonElement>("button")].find((x) =>
    text.test(x.textContent ?? ""),
  );
  if (!b) throw new Error(`no button matching ${text}: ${host.textContent}`);
  return b;
}

describe("update panel", () => {
  // Real timers: the panel polls every 2s, so these wait it out. Slow for a
  // unit test and worth it — the bug was entirely about time passing and
  // nothing happening.
  it("keeps asking after a download starts, and notices when it is staged", { timeout: 20000 }, async () => {
    await render();
    await act(async () => button(/Download and install/).click());
    await settle();

    const afterStart = statusReads;
    // The download runs in the background, so the only way the panel learns
    // anything is by asking again.
    await settle(2500);
    expect(statusReads).toBeGreaterThan(afterStart);

    // The server finishes: staged appears.
    statusBody = { ...base, staged: "v0.6.14", staged_at: 1 };
    await settle(2500);
    expect(host.textContent).toContain("ready to install");

    // And it stops polling once there is nothing to wait for — a panel that
    // keeps hitting an admin endpoint every two seconds for ever is its own
    // bug.
    const afterStaged = statusReads;
    await settle(2500);
    expect(statusReads).toBe(afterStaged);
  });

  it("stops waiting when the download fails rather than saying Downloading for ever", { timeout: 20000 }, async () => {
    await render();
    await act(async () => button(/Download and install/).click());
    await settle();

    statusBody = { ...base, download_error: "checksum did not match" };
    await settle(2500);

    expect(host.textContent).toContain("checksum did not match");
    const settled = statusReads;
    await settle(2500);
    expect(statusReads).toBe(settled);
  });
});
