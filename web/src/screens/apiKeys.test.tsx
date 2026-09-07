/*
 * The API keys panel (ADR 0061).
 *
 * jsdom sees no layout, so this is about what the screen *says*. Two of the
 * three things below are the difference between a usable credential and a
 * support question:
 *
 *   the secret is shown, and shown to be un-repeatable
 *   a key nothing has ever used says "never", not a date in 1970
 *
 * The second is not cosmetic. `last_used` is the column somebody reads when
 * deciding which key to revoke after a suspected theft, and "1 Jan 1970" beside
 * every unused key makes the one honest signal on the screen unreadable.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { Settings } from "./Settings";
import { apiKeysPollMs } from "@/api/hooks";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

type Key = { id: string; name: string; created_at: number; last_used: number };

let host: HTMLDivElement;
let root: Root;
let posted: { url: string; body: string }[];
let keys: Key[];

function mount(initial: Key[]) {
  keys = initial;
  posted = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });

      if (url.includes("/api/auth/status")) {
        return json({
          authenticated: true,
          configured: true,
          user: { id: "u1", name: "chris", role: "admin" },
        });
      }
      if (url.includes("/api/keys")) {
        if (method === "POST") {
          posted.push({ url, body: String(init?.body ?? "") });
          const made = {
            id: "k2",
            name: JSON.parse(String(init?.body)).name,
            created_at: 1788500000,
            last_used: 0,
          };
          keys = [made, ...keys];
          return json({ key: made, secret: "SECRET-VALUE-ONLY-ONCE" });
        }
        if (method === "DELETE") {
          posted.push({ url, body: "" });
          keys = [];
          return json({ revoked: true });
        }
        return json({ keys });
      }
      return json({});
    }),
  );
}

beforeEach(() => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  vi.useRealTimers();
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function render() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <MemoryRouter initialEntries={["/settings"]}>
            <Settings />
          </MemoryRouter>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await settle();
  // The keys live in the Account pane, which is not the one Settings opens on.
  const account = [...host.querySelectorAll("button")].find(
    (b) => b.textContent?.trim() === "Account",
  );
  if (account) {
    await act(async () => account.click());
    await settle();
  }
}

async function settle() {
  for (let i = 0; i < 6; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

const text = () => host.textContent ?? "";

describe("the API keys panel", () => {
  // A key nothing has ever presented says so. This is the column somebody
  // reads when deciding what is safe to revoke.
  it("says a never-used key was never used, rather than dating it to 1970", async () => {
    mount([
      { id: "k1", name: "backup script", created_at: 1788500000, last_used: 0 },
    ]);
    await render();

    expect(text()).toContain("backup script");
    expect(text()).toContain("Never used");
    expect(text()).not.toContain("1970");
  });

  /*
   * The secret is shown, and the screen says it will not be shown again.
   *
   * Both halves matter. Without the value the key is unusable; without the
   * warning somebody closes the panel and finds out later that the server
   * genuinely cannot hand it back — which reads as data loss rather than as the
   * design working.
   */
  it("shows a new secret once and says it will not be shown again", async () => {
    mount([]);
    await render();

    const input = host.querySelector(
      'input[placeholder*="backup script"]',
    ) as HTMLInputElement;
    expect(input).toBeTruthy();

    await act(async () => {
      const setter = Object.getOwnPropertyDescriptor(
        window.HTMLInputElement.prototype,
        "value",
      )!.set!;
      setter.call(input, "deploy bot");
      input.dispatchEvent(new Event("input", { bubbles: true }));
    });

    const submit = [...host.querySelectorAll("button")].find(
      (b) => b.textContent?.includes("Create a key"),
    )!;
    await act(async () => submit.click());
    await settle();

    expect(posted.some((p) => p.body.includes("deploy bot"))).toBe(true);
    expect(text()).toContain("SECRET-VALUE-ONLY-ONCE");
    expect(text()).toContain("not shown again");
  });

  /*
   * A key used elsewhere stops saying "Never used" without anybody touching the
   * screen.
   *
   * This is the bug the live verification found. `last_used` is changed by a
   * script on another machine, so no mutation in this client can invalidate it,
   * and the create/revoke invalidations do not cover it — the panel went on
   * saying "Never used" with the database holding a timestamp a minute old.
   * Wrong in the direction that matters: it is the column somebody reads when
   * deciding which key is safe to revoke.
   */
  it("notices a key that was used while the panel was open", async () => {
    mount([
      { id: "k1", name: "backup script", created_at: 1788500000, last_used: 0 },
    ]);
    await render();
    expect(text()).toContain("Never used");

    // Something else presents the key. Nothing happens in this client at all.
    keys = [
      {
        id: "k1",
        name: "backup script",
        created_at: 1788500000,
        last_used: 1788795309,
      },
    ];

    await act(async () => {
      await vi.advanceTimersByTimeAsync(apiKeysPollMs + 1000);
    });
    await settle();

    expect(text()).not.toContain("Never used");
    expect(text()).toContain("Last used");
  });

  // Revoking asks the server to revoke, rather than only hiding the row.
  it("revokes through the server", async () => {
    mount([
      { id: "k1", name: "old key", created_at: 1788500000, last_used: 1788500900 },
    ]);
    await render();

    const revoke = [...host.querySelectorAll("button")].find(
      (b) => b.textContent?.trim() === "Revoke",
    )!;
    expect(revoke).toBeTruthy();
    await act(async () => revoke.click());
    await settle();

    expect(posted.some((p) => p.url.includes("/api/keys/k1"))).toBe(true);
  });
});
