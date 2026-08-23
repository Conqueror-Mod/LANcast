/*
 * The banner that says this window is older than its server.
 *
 * It used to tell everyone to close and reopen whenever the two versions
 * differed. That is only true when an update is *staged* — downloaded, verified
 * and waiting to be applied on the next start. With nothing staged, reopening
 * runs the same binary, the versions still differ, and the banner comes back:
 * advice that cannot work, repeating for ever.
 *
 * Reported by somebody who had already restarted and was still being told to.
 *
 * The two states are genuinely different problems. Staged is a chore. Nothing
 * staged means the window and the server came from different installs, which a
 * restart cannot reconcile.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { AppShell } from "./AppShell";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;

/**
 * clientVersion is what the desktop shell reports about itself; staged is what
 * the server says is waiting, or undefined for nothing.
 */
function mount(opts: { clientVersion?: string; staged?: string; server?: string }) {
  const server = opts.server ?? "0.8.1";
  // The desktop bridge. Absent in a browser, where the page *is* the client.
  (window as unknown as Record<string, unknown>).lancastDesktopState =
    opts.clientVersion === undefined
      ? undefined
      : async () => ({ client_version: opts.clientVersion });

  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/api/health")) {
        return json({ status: "ok", version: server, api_version: 1 });
      }
      if (url.includes("/api/activity")) {
        return json({
          active: false,
          tasks: [],
          ...(opts.staged ? { staged: opts.staged } : {}),
        });
      }
      if (url.includes("/api/auth/status")) {
        return json({
          authenticated: true,
          configured: true,
          user: { id: "u1", name: "chris", role: "admin" },
        });
      }
      if (url.includes("/api/libraries")) return json([]);
      return json({ items: [], total: 0 });
    }),
  );
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
  delete (window as unknown as Record<string, unknown>).lancastDesktopState;
});

async function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <MemoryRouter initialEntries={["/"]}>
            <AppShell>
              <div />
            </AppShell>
          </MemoryRouter>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  for (let i = 0; i < 5; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

function banner(): string {
  const el = host.querySelector('[role="status"]');
  return el?.textContent ?? "";
}

describe("the stale-window banner", () => {
  it("says nothing when the window matches the server", async () => {
    mount({ clientVersion: "0.8.1", server: "0.8.1" });
    await render();
    expect(banner()).toBe("");
  });

  // The original behaviour, and correct in this case only.
  it("asks for a restart when an update is staged", async () => {
    mount({ clientVersion: "0.8.0", server: "0.8.1", staged: "0.8.1" });
    await render();
    expect(banner()).toContain("Close LANcast and open it again");
  });

  /*
   * The bug. Nothing staged means a restart changes nothing, so telling
   * somebody to restart guarantees they will be told again.
   */
  it("does not ask for a restart when nothing is staged", async () => {
    mount({ clientVersion: "0.8.0", server: "0.8.1" });
    await render();
    expect(banner()).not.toBe("");
    expect(banner()).not.toContain("Close LANcast and open it again");
    expect(banner()).toContain("Reinstalling");
  });

  // A browser tab has no desktop shell and no version of its own: the page
  // arrived from the server it would be complaining about.
  it("says nothing in a browser", async () => {
    mount({ server: "0.8.1", staged: "0.8.1" });
    await render();
    expect(banner()).toBe("");
  });

  // A development build has no ordering against a release, so a difference
  // between them means nothing and must not nag whoever is working on it.
  it("says nothing for a development build", async () => {
    mount({ clientVersion: "dev", server: "0.8.1" });
    await render();
    expect(banner()).toBe("");
  });
});
