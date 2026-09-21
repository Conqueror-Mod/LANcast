/*
 * The switch that says whether other servers may list you.
 *
 * It shipped in a draft showing **on** while the server held **off**, which
 * for a privacy control is the worst available failure: somebody reads the
 * screen and believes they opted in when they did not. The cause was the
 * mistake CLAUDE.md calls the most-repeated one here — the write invalidated
 * `["me"]`, which matches no query, so the switch never reconciled with the
 * server and kept showing whatever was last clicked.
 *
 * Nothing failed. The request succeeded, the server was right, and only the
 * picture was stale. So these tests are about the picture.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { Settings } from "./Settings";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
/** What the server currently holds. Writes move it; the UI must follow. */
let visible: boolean;
let writeFails: boolean;
let writes: boolean[];
let client: QueryClient;

/** How many times the signed-in user has been read from the server. */
function authReads(): number {
  return (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls.filter(
    ([u]) => String(u).includes("/api/auth/status"),
  ).length;
}

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  visible = false;
  writeFails = false;
  writes = [];

  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      const json = (v: unknown, status = 200) =>
        new Response(JSON.stringify(v), {
          status,
          headers: { "Content-Type": "application/json" },
        });

      if (url.includes("/api/profile/peer-visibility")) {
        const body = JSON.parse(String(init?.body));
        writes.push(body.visible);
        if (writeFails) {
          return json(
            { error: { code: "error", message: "no" } },
            500,
          );
        }
        visible = body.visible;
        return json({ ok: true });
      }
      if (url.includes("/api/auth/status")) {
        return json({
          authenticated: true,
          configured: true,
          user: {
            id: "local",
            name: "Conqueror",
            role: "admin",
            sharing: true,
            visible_to_peers: visible,
          },
        });
      }
      if (method !== "GET") return json({ ok: true });
      if (url.includes("/api/libraries")) return json([]);
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
  client = new QueryClient({
    // No caching window: this test is about whether a refetch happens at all,
    // and a stale time would decide that instead of the invalidation.
    defaultOptions: { queries: { retry: false, staleTime: 0, gcTime: 0 } },
  });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={["/settings?pane=account"]}>
          <Settings />
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
  await settle();
}

async function settle() {
  for (let i = 0; i < 4; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 10));
    });
  }
}

/** The peer-visibility box, found by its label rather than by position. */
function box(): HTMLInputElement {
  const label = [...host.querySelectorAll("label")].find((l) =>
    (l.textContent ?? "").includes("Let paired servers list me"),
  );
  if (!label) throw new Error("the peer visibility switch is not on this pane");
  return label.querySelector("input")!;
}

describe("let paired servers list me", () => {
  it("is off when the server says off", async () => {
    await render();
    expect(box().checked).toBe(false);
  });

  it("is on when the server says on", async () => {
    visible = true;
    await render();
    expect(box().checked).toBe(true);
  });

  /*
   * The one that failed, and the assertion that actually catches it.
   *
   * Checking that the box looks right afterwards is not enough: the component
   * holds an optimistic `pending` value, so it shows what was clicked whether
   * or not anything reconciled. A first version of this test passed with the
   * broken key still in place, which is worse than no test.
   *
   * What distinguishes them is whether the user was **re-read** after the
   * write. With the right key the query is invalidated and fetched again; with
   * `["me"]` nothing matches and no fetch happens.
   */
  it("re-reads the user after writing, so the switch can disagree", async () => {
    await render();
    const before = authReads();

    await act(async () => {
      box().click();
    });
    await settle();

    expect(writes).toEqual([true]);
    expect(visible).toBe(true);
    expect(authReads()).toBeGreaterThan(before);
  });

  /*
   * And the consequence, stated directly: if the server disagrees with what
   * was clicked, the server wins. This is what a stale picture actually costs
   * on a privacy switch.
   */
  it("follows the server when the server disagrees with the click", async () => {
    await render();
    await act(async () => {
      box().click();
    });
    await settle();
    expect(box().checked).toBe(true);

    // Something else turned it off — another device, an administrator action,
    // a failed write elsewhere. The next read must win.
    visible = false;
    await act(async () => {
      client.invalidateQueries({ queryKey: ["auth-status"] });
    });
    await settle();
    expect(box().checked).toBe(false);
  });

  it("still agrees with the server after being turned off", async () => {
    visible = true;
    await render();
    await act(async () => {
      box().click();
    });
    await settle();

    expect(writes).toEqual([false]);
    expect(visible).toBe(false);
    expect(box().checked).toBe(false);
  });

  /*
   * A refused write must not leave the switch claiming a state the server
   * does not hold. For a privacy control that is worse than showing an error,
   * because somebody reads it and believes it.
   */
  it("goes back to what the server holds when the write fails", async () => {
    writeFails = true;
    await render();
    expect(box().checked).toBe(false);

    await act(async () => {
      box().click();
    });
    await settle();

    expect(visible).toBe(false);
    expect(box().checked).toBe(false);
  });

  // It is its own decision, never a wider setting of the one beside it:
  // agreeing to publish what you finished is not agreeing to be watched live.
  it("is a separate switch from the watched-history one", async () => {
    await render();
    const labels = [...host.querySelectorAll("label")].filter((l) =>
      (l.textContent ?? "").includes("Let "),
    );
    expect(labels.length).toBeGreaterThanOrEqual(2);
    expect(box()).not.toBe(
      labels
        .find((l) => (l.textContent ?? "").includes("what I have watched"))!
        .querySelector("input"),
    );
  });
});
