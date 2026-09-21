/*
 * Pairing, from the screen that never existed.
 *
 * `internal/peer` and every route it serves shipped in August, and nothing in
 * the client called any of them — so two servers could not be introduced, and
 * the People screen's peers section (built and correct since ADR 0045) could
 * never show a single person. These tests are about the wiring, which is the
 * thing that was missing and the thing this project has been caught by before:
 * a settings shell whose panes were not connected to its buttons.
 *
 * jsdom performs no layout, so nothing here proves the pane looks right.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { PeerSettings } from "./PeerSettings";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

// Invented throughout. A real fingerprint never appears in a fixture.
const PAIRED = {
  fingerprint: "BBBB4G6XEG33AUPJU7LGSTT4N74U6D4B",
  fingerprint_display: "BBBB-4G6X-EG33-AUPJ-U7LG-STT4-N74U-6D4B",
  name: "Georgia",
  state: "paired",
  addrs: ["10.0.0.14:8080"],
  added_at: 1_757_000_000,
  last_seen: Math.floor(Date.now() / 1000) - 600,
};
const ADDED = {
  ...PAIRED,
  fingerprint: "CCCC4G6XEG33AUPJU7LGSTT4N74U6D4B",
  fingerprint_display: "CCCC-4G6X-EG33-AUPJ-U7LG-STT4-N74U-6D4B",
  name: "Front room",
  state: "added",
  last_seen: 0,
};

const SILENT = {
  ...PAIRED,
  fingerprint: "DDDD4G6XEG33AUPJU7LGSTT4N74U6D4B",
  fingerprint_display: "DDDD-4G6X-EG33-AUPJ-U7LG-STT4-N74U-6D4B",
  name: "Loft",
  state: "paired",
  last_seen: 0,
};

let host: HTMLDivElement;
let root: Root;
let peers: unknown[];
let writes: { url: string; method: string; body: unknown }[];
let inviteStatus: number;

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  peers = [];
  writes = [];
  inviteStatus = 200;

  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      const json = (v: unknown, status = 200) =>
        new Response(JSON.stringify(v), {
          status,
          headers: { "Content-Type": "application/json" },
        });

      if (method !== "GET") {
        writes.push({
          url,
          method,
          body: init?.body ? JSON.parse(String(init.body)) : undefined,
        });
        return json({ ok: true });
      }
      if (url.includes("/api/peers/invite")) {
        if (inviteStatus !== 200) {
          // The real envelope nests them: {error: {code, message}}. A flat
          // fixture passes a test that proves nothing, because the client
          // would fall back to res.statusText and still render *something*.
          return json(
            {
              error: {
                code: "not_reachable",
                message:
                  "This server has no address another machine could reach.",
              },
            },
            inviteStatus,
          );
        }
        return json({
          invite: "lancast-invite-AAAA",
          fingerprint: "AAAA4G6X",
          fingerprint_display: "AAAA-4G6X",
          name: "Aither",
          addrs: ["192.168.1.66:8080"],
        });
      }
      if (url.includes("/api/peers")) return json({ peers });
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
          <PeerSettings />
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
  for (let i = 0; i < 3; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

function button(label: string): HTMLButtonElement | undefined {
  return [...host.querySelectorAll("button")].find((b) =>
    (b.textContent ?? "").includes(label),
  ) as HTMLButtonElement | undefined;
}

async function click(el: HTMLElement) {
  await act(async () => {
    el.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  });
  // Several flushes: a click here can enable a query that was not running,
  // and its fetch has to resolve and re-render before anything is assertable.
  for (let i = 0; i < 4; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 10));
    });
  }
}

describe("pairing with another server", () => {
  it("says plainly that pairing grants nothing", async () => {
    await render();
    // The single most load-bearing sentence on the pane: somebody who thinks
    // pairing shares their library will pair with the wrong people.
    expect(host.textContent).toContain("grants nothing on its own");
  });

  it("does not fetch the invite until asked", async () => {
    await render();
    const calls = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls;
    expect(calls.some(([u]) => String(u).includes("/invite"))).toBe(false);

    await click(button("Show invite")!);
    const after = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls;
    expect(after.some(([u]) => String(u).includes("/invite"))).toBe(true);
    expect(host.textContent).toContain("lancast-invite-AAAA");
  });

  /*
   * A loopback-only server cannot introduce itself, and the server says so
   * with 409 not_reachable. That is an answer, not a fault, and it has to
   * reach the person who just asked for an invite.
   */
  it("shows the server's own reason when it cannot introduce itself", async () => {
    inviteStatus = 409;
    await render();
    await click(button("Show invite")!);
    expect(host.textContent).toContain("no address another machine could reach");
  });

  it("posts a pasted invite and clears the box", async () => {
    await render();
    const input = host.querySelector<HTMLInputElement>("input.set-input")!;
    await act(async () => {
      const setter = Object.getOwnPropertyDescriptor(
        window.HTMLInputElement.prototype,
        "value",
      )!.set!;
      setter.call(input, "lancast-invite-BBBB");
      input.dispatchEvent(new Event("input", { bubbles: true }));
    });
    await click(button("Add")!);

    const post = writes.find((w) => w.method === "POST");
    expect(post?.url).toContain("/api/peers");
    expect(post?.body).toEqual({ invite: "lancast-invite-BBBB" });
    expect(input.value).toBe("");
  });

  /*
   * `added` is not `paired`. Only the transport can move a peer to paired, by
   * reaching the far side and finding it holds us too (ADR 0044 §3), so
   * rendering `added` as connected would claim the other person agreed to
   * something they have not done.
   */
  it("does not claim a one-sided pairing is mutual", async () => {
    peers = [ADDED];
    await render();
    expect(host.textContent).toContain("not yet mutual");
    expect(host.textContent).toContain("they need to add your invite too");
  });

  // Never answered and answered-then-quiet are different facts.
  it("tells a peer that has never answered from one that has", async () => {
    peers = [SILENT, PAIRED];
    await render();
    const text = host.textContent ?? "";
    expect(text).toContain("Has not answered yet");
    expect(text).toContain("Last answered");
  });

  // Unpairing is revocation — everything the peer was granted goes with it —
  // so it asks first rather than firing on one click.
  it("confirms before unpairing", async () => {
    peers = [PAIRED];
    await render();

    await click(button("Unpair")!);
    expect(writes.filter((w) => w.method === "DELETE")).toHaveLength(0);

    await click(button("Remove for good")!);
    const del = writes.find((w) => w.method === "DELETE");
    expect(del?.url).toContain(encodeURIComponent(PAIRED.fingerprint));
  });

  it("says what to do when nothing is paired", async () => {
    await render();
    expect(host.textContent).toContain("Send somebody your invite");
  });
});
