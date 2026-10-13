/*
 * Authorized devices: every device signed in, named, with the one in use
 * marked, and Sign out — your own in Security, everyone's for an
 * administrator.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AuthorizedDevices } from "./AuthorizedDevices";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let deletes: string[];

const now = Math.floor(Date.now() / 1000);
const mine = [
  { id: "aaaaaaaaaaaaaaaa", user_id: "u_1", user_name: "Chris", device: "LANcast app on Windows", address: "192.168.1.20", created_at: now - 9000, last_seen: now, current: true },
  { id: "bbbbbbbbbbbbbbbb", user_id: "u_1", user_name: "Chris", device: "Chrome on Android", address: "10.0.0.5", created_at: now - 90000, last_seen: now - 3600, current: false },
  { id: "cccccccccccccccc", user_id: "u_1", user_name: "Chris", device: "", created_at: now - 900000, last_seen: now - 86400 * 3, current: false },
];
const everyone = [...mine, { id: "dddddddddddddddd", user_id: "u_2", user_name: "Georgia", device: "Safari on iPhone", created_at: now, last_seen: now, current: false }];

beforeEach(() => {
  deletes = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (init?.method === "DELETE") {
        deletes.push(url);
        return new Response(null, { status: 204 });
      }
      const body = url === "/api/sessions" ? everyone : mine;
      return new Response(JSON.stringify({ sessions: body }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
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
});

async function render(all = false) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <AuthorizedDevices everyone={all} />
      </QueryClientProvider>,
    );
  });
  for (let i = 0; i < 4; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

const rows = () => [...host.querySelectorAll<HTMLElement>(".devices__row")];

describe("authorized devices", () => {
  it("names each device and marks the one in use", async () => {
    await render();
    expect(rows()).toHaveLength(3);
    expect(rows()[0].textContent).toContain("LANcast app on Windows");
    expect(rows()[0].textContent).toContain("This device");
    expect(rows()[0].textContent).toContain("In use now");
    expect(rows()[1].textContent).toContain("Chrome on Android");
    expect(rows()[1].textContent).not.toContain("This device");
    // A session from before devices were recorded still says something.
    expect(rows()[2].textContent).toContain("signed in before this was recorded");
  });

  it("signs out one of your devices through your own route", async () => {
    await render();
    await act(async () => rows()[1].querySelector("button")!.click());
    expect(deletes).toEqual(["/api/profile/sessions/bbbbbbbbbbbbbbbb"]);
  });

  it("shows an administrator every account's devices, and signs out through the admin route", async () => {
    await render(true);
    expect(rows()).toHaveLength(4);
    expect(rows()[3].textContent).toContain("Georgia");
    await act(async () => rows()[3].querySelector("button")!.click());
    expect(deletes).toEqual(["/api/sessions/dddddddddddddddd"]);
  });
});
