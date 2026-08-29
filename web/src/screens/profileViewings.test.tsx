/*
 * The viewings card, and when it must stay away.
 *
 * The server counts viewings from schema revision 32, and a second number
 * beside "Finished" is only worth the space when it says something different.
 * Most profiles have no rewatches in them, and a card repeating the one next to
 * it is noise on the common case to serve the uncommon one.
 *
 * The absent case is the one worth pinning. A server too old to send the field
 * is ordinary — a client can be newer than what it talks to — and rendering
 * "0 viewings" beside "412 finished" would be a wrong statement rather than a
 * missing one.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { Profile } from "./Profile";
import type { ProfileStats } from "@/api/types";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;

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

async function renderProfile(stats: Partial<ProfileStats>) {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            user: { name: "Conqueror", admin: true, secured: true },
            stats: {
              started: 20,
              finished: 12,
              watched_ms: 3_600_000,
              first_at: null,
              ...stats,
            },
            history: [],
            total: 0,
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
    ),
  );
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <Profile />
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
  await act(async () => {
    await new Promise((r) => setTimeout(r, 5));
  });
}

describe("the viewings card", () => {
  it("appears once something has been watched more than once", async () => {
    await renderProfile({ finished: 12, viewings: 20 });
    expect(host.textContent).toContain("Viewings");
    expect(host.textContent).toContain("20");
  });

  it("stays away when nothing has been rewatched", async () => {
    // Equal numbers mean the card beside it already said this.
    await renderProfile({ finished: 12, viewings: 12 });
    expect(host.textContent).not.toContain("Viewings");
  });

  it("stays away on a server too old to count them", async () => {
    await renderProfile({ finished: 12 });
    expect(host.textContent).not.toContain("Viewings");
  });

  /*
   * The note under "Time watched" described the old behaviour, and a label that
   * describes an older version of the thing it labels is worse than none: it is
   * confidently wrong rather than silent.
   */
  it("no longer claims time is counted once per title", async () => {
    await renderProfile({ finished: 12, viewings: 20 });
    expect(host.textContent).not.toContain("counted once per title");
    expect(host.textContent).toContain("every viewing counted");
  });
});
