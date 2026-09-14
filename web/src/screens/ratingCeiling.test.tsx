/*
 * The content rating ceiling, in Settings → Users (ADR 0015).
 *
 * jsdom cannot prove the rule — that lives on the server, in the listing and in
 * playback authorisation, where a client-side hide would only ever be a
 * suggestion. What it can prove is the wiring, and one decision worth guarding:
 * the control is not offered for your own account, because an administrator can
 * lift any ceiling and one set on yourself is a note rather than a limit.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { Settings } from "./Settings";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let sent: Array<{ url: string; method: string; body: unknown }>;

const me = { id: "u_admin", name: "chris", role: "admin" };
const child = { id: "u_kid", name: "kiddo", role: "member" };

function stub(kidCeiling?: string) {
  sent = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const u = String(url);
      const method = init?.method ?? "GET";
      sent.push({
        url: u,
        method,
        body: init?.body ? JSON.parse(String(init.body)) : undefined,
      });
      if (method !== "GET") return new Response(null, { status: 204 });

      let body: unknown = {};
      if (u.includes("/api/auth/status")) {
        // How the shell learns who is asking, and that they may see this pane.
        body = {
          configured: true,
          authenticated: true,
          lan_enabled: false,
          restart_required: false,
          user: me,
        };
      } else if (u.startsWith("/api/users")) {
        body = {
          users: [
            me,
            kidCeiling ? { ...child, max_content_rating: kidCeiling } : child,
          ],
        };
      } else if (u.startsWith("/api/libraries")) {
        body = { libraries: [] };
      }
      return new Response(JSON.stringify(body), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }),
  );
}

async function renderUsersPane() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <MemoryRouter initialEntries={["/settings?pane=users"]}>
            <Settings />
          </MemoryRouter>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await settle();
}

/*
 * Wait for a condition rather than ticking a fixed number of times.
 *
 * The same trap the sharing-toggle suite documents: a fixed flush is enough
 * when the file runs alone and not enough under a loaded suite, and the case
 * that fails that way is the one asserting something is *present* — an absence
 * agrees with an unresolved query for the wrong reason.
 */
async function settle() {
  for (let i = 0; i < 6; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

async function waitFor(cond: () => boolean, what: string) {
  for (let i = 0; i < 100; i++) {
    if (cond()) return;
    await act(async () => {
      await new Promise((r) => setTimeout(r, 10));
    });
  }
  throw new Error(`timed out waiting for ${what}`);
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
});

const ceilingFor = (name: string) =>
  host.querySelector<HTMLSelectElement>(
    `select[aria-label="Content rating limit for ${name}"]`,
  );

describe("a limit set for an account", () => {
  it("is offered on another account", async () => {
    stub();
    await renderUsersPane();
    await waitFor(() => !!ceilingFor("kiddo"), "the limit control");
    expect(ceilingFor("kiddo")).toBeTruthy();
  });

  it("is not offered on your own", async () => {
    /*
     * An administrator can lift any ceiling, so one set on your own account is
     * a note rather than a limit. Offering the control would suggest otherwise,
     * which is the kind of half-true affordance that gets relied on.
     */
    stub();
    await renderUsersPane();
    // Waited for, so this is an absence beside a present control rather than
    // an absence because nothing had rendered yet.
    await waitFor(() => !!ceilingFor("kiddo"), "the other account's control");
    expect(ceilingFor("chris")).toBeNull();
  });

  it("shows what is already in force without opening anything", async () => {
    // The question when this pane opens is "which of these accounts is
    // limited", and a limit you must click each row to discover is one nobody
    // audits.
    stub("PG");
    await renderUsersPane();
    await waitFor(() => !!ceilingFor("kiddo"), "the limit control");
    expect(host.textContent).toContain("PG and under");
    expect(ceilingFor("kiddo")!.value).toBe("PG");
  });

  it("sends the label the server names it by", async () => {
    stub();
    await renderUsersPane();
    await waitFor(() => !!ceilingFor("kiddo"), "the limit control");

    const select = ceilingFor("kiddo")!;
    await act(async () => {
      select.value = "PG-13";
      select.dispatchEvent(new Event("change", { bubbles: true }));
    });
    await settle();

    const patch = sent.find((s) => s.method === "PATCH");
    expect(patch?.url).toBe("/api/users/u_kid");
    expect(patch?.body).toEqual({ max_content_rating: "PG-13" });
  });

  it("clears with an empty string rather than a word", async () => {
    // "No limit" is the absence of a label, not a label meaning none — the
    // server would refuse a word it cannot place on its scale, which is the
    // right behaviour and the wrong error to provoke from a dropdown.
    stub("PG");
    await renderUsersPane();
    await waitFor(() => !!ceilingFor("kiddo"), "the limit control");

    const select = ceilingFor("kiddo")!;
    await act(async () => {
      select.value = "";
      select.dispatchEvent(new Event("change", { bubbles: true }));
    });
    await settle();

    expect(sent.find((s) => s.method === "PATCH")?.body).toEqual({
      max_content_rating: "",
    });
  });
});
