/*
 * The downloads page with nothing on it.
 *
 * Reported as "a blank gap on the Downloads page", and it was two things
 * stacked: the shared empty-state paragraph carries 40px of vertical padding —
 * right in a grid where it stands alone, wrong directly beneath a note already
 * explaining the page — and the list container rendered even with no rows,
 * adding its own padding below that.
 *
 * The container is what a test can hold onto. Spacing is CSS and belongs to the
 * eye; an element that exists to hold nothing is a fact.
 *
 * Each case re-imports the module, because `readDevice` memoises in a
 * module-level cache: seeding localStorage after something has already read the
 * key is invisible, and a test that did so would be asserting against the
 * previous case's empty list rather than the one it set up.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { MemoryRouter } from "react-router";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  vi.resetModules();
  window.localStorage.clear();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  window.localStorage.clear();
});

// The receipts are kept per account (R13-8), so the page has to know who is
// signed in. React Query is imported alongside the page, after the module
// reset, so both see the same instance of its context.
async function render(userId = "u1") {
  const { Downloads } = await import("./Downloads");
  const { QueryClient, QueryClientProvider } = await import(
    "@tanstack/react-query"
  );
  const client = new QueryClient();
  client.setQueryData(["auth-status"], {
    configured: true,
    authenticated: true,
    user: { id: userId, name: userId, role: "member" },
  });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <Downloads />
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
}

async function seed(userId = "u1") {
  const { downloadsKey } = await import("@/lib/downloads");
  window.localStorage.setItem(
    downloadsKey(userId),
    JSON.stringify([
      { itemId: 1, title: "A Film", filename: "A Film.mkv", at: Date.now() },
    ]),
  );
}

describe("the downloads page", () => {
  it("does not render an empty list container", async () => {
    await render();
    expect(host.querySelector(".downloads__row")).toBeNull();
    expect(host.querySelector(".downloads__list")).toBeNull();
  });

  it("still explains itself when there is nothing to show", async () => {
    await render();
    expect(host.textContent).toContain("Nothing yet");
  });

  /*
   * The guard on the case above: if the storage key or the row class ever
   * changes, the empty-container assertion would pass for the wrong reason —
   * a page rendering no rows because nothing reached it looks identical to a
   * page correctly omitting an empty container.
   */
  it("renders the list once there is something in it", async () => {
    await seed();
    await render();

    expect(host.querySelectorAll(".downloads__row").length).toBe(1);
    expect(host.querySelector(".downloads__list")).not.toBeNull();
    expect(host.textContent).not.toContain("Nothing yet");
  });

  /*
   * One device, two accounts: somebody else signing in on the family laptop
   * does not see what the first person downloaded (R13-8). On main the list
   * was one key for the whole device, so a child's account was shown a
   * parent's titles past its own rating ceiling.
   */
  it("shows only the signed-in account's receipts", async () => {
    await seed("parent");
    await render("child");
    expect(host.querySelectorAll(".downloads__row").length).toBe(0);
    expect(host.textContent).not.toContain("A Film");
  });
});
