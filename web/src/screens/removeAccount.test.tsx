/*
 * Removing an account, in Settings → Users (R15-1).
 *
 * It was one press: the DELETE went on the click, from a button drawn just
 * after "Remove picture". Now the press asks, in the page, naming the account
 * and what goes with it, and only the second press sends anything.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { FocusProvider } from "@/focus/FocusController";
import { Settings } from "./Settings";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let sent: Array<{ url: string; method: string }>;

const me = { id: "u_admin", name: "dana", role: "admin" };
const other = { id: "u_kid", name: "kiddo", role: "member", picture: "abc" };

function stub() {
  sent = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const u = String(url);
      const method = init?.method ?? "GET";
      sent.push({ url: u, method });
      if (method !== "GET") return new Response(null, { status: 204 });
      let body: unknown = {};
      if (u.includes("/api/auth/status")) {
        body = { configured: true, authenticated: true, lan_enabled: false, restart_required: false, user: me };
      } else if (u.startsWith("/api/users")) {
        body = { users: [me, other] };
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

async function render() {
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

const button = (text: string) =>
  [...host.querySelectorAll("button")].find((b) => b.textContent?.trim() === text);
const deletes = () => sent.filter((s) => s.method === "DELETE" && s.url === "/api/users/u_kid");

async function press(el: HTMLElement) {
  await act(async () => {
    el.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  });
}

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  stub();
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

const removeButton = () =>
  [...host.querySelectorAll<HTMLButtonElement>("button.set-btn--danger")].find((b) =>
    b.textContent?.startsWith("Remove"),
  );

describe("removing somebody's account", () => {
  it("asks first, naming them and what goes, and sends nothing on the first press", async () => {
    await render();
    await waitFor(() => !!removeButton(), "the remove button");
    await press(removeButton()!);

    expect(deletes()).toHaveLength(0);
    const dialog = host.querySelector('[role="alertdialog"]');
    expect(dialog).toBeTruthy();
    expect(dialog!.getAttribute("aria-label")).toBe("Remove kiddo");
    expect(dialog!.textContent).toContain("Remove kiddo?");
    expect(dialog!.textContent).toMatch(/watched/);
    expect(dialog!.textContent).toMatch(/cannot be undone/);
    // Keep is where focus lands, so OK by habit keeps the account.
    expect(document.activeElement?.textContent).toBe("Keep kiddo");
  });

  it("keeps the account on Keep, and removes it only on the second press", async () => {
    await render();
    await waitFor(() => !!removeButton(), "the remove button");

    await press(removeButton()!);
    await press(button("Keep kiddo")!);
    expect(host.querySelector('[role="alertdialog"]')).toBeNull();
    expect(deletes()).toHaveLength(0);

    await press(removeButton()!);
    await press(button("Remove kiddo")!);
    await waitFor(() => deletes().length === 1, "the delete");
    expect(deletes()).toHaveLength(1);
  });

  it("keeps the account on Back", async () => {
    await render();
    await waitFor(() => !!removeButton(), "the remove button");
    await press(removeButton()!);
    await act(async () => {
      document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    });
    expect(host.querySelector('[role="alertdialog"]')).toBeNull();
    expect(deletes()).toHaveLength(0);
  });
});
