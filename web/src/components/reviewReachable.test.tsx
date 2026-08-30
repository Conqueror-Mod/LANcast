/*
 * The way in to the Review screen.
 *
 * Review holds two queues. The match queue empties as things are confirmed; the
 * collision report does not empty on its own, because a shared identity is
 * reported and never resolved (ADR 0042). The nav link counted only the first,
 * so a library with nothing left to confirm and two files still claiming one
 * work lost its only route to the report — reported as *"there are still
 * entries for two files one work, but we can only see Review if there is a
 * fixable entry"*.
 *
 * What makes it worth a test rather than a one-line fix is that the screen had
 * already thought about this case and the nav had not: Review.tsx carries the
 * comment "Nothing to review would be a lie with a collision report below it",
 * and was right, while the link that reaches it disagreed.
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

type Opts = {
  /** Rows in the match queue — what /api/review counts. */
  review?: number;
  /** Collision groups, and whether each has been accepted already. */
  collisions?: { dismissed?: boolean }[];
  role?: string;
};

function mount(opts: Opts) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/api/health")) {
        return json({ status: "ok", version: "0.8.32", api_version: 1 });
      }
      if (url.includes("/api/activity")) return json({ active: false, tasks: [] });
      if (url.includes("/api/auth/status")) {
        return json({
          authenticated: true,
          configured: true,
          user: { id: "u1", name: "chris", role: opts.role ?? "admin" },
        });
      }
      if (url.includes("/api/libraries")) return json([]);
      if (url.includes("/api/collisions")) {
        return json({
          collisions: (opts.collisions ?? []).map((c, i) => ({
            provider: "tmdb",
            external_id: String(324857 + i),
            same_size: false,
            members: [
              { id: i * 2 + 1, title: "A", path: "X:\\a.mkv" },
              { id: i * 2 + 2, title: "A (Alternate Cut)", path: "X:\\b.mkv" },
            ],
            ...(c.dismissed ? { dismissed_at: 1756500000 } : {}),
          })),
        });
      }
      if (url.includes("/api/review")) {
        const n = opts.review ?? 0;
        return json({
          items: Array.from({ length: n }, (_, i) => ({
            id: 900 + i,
            title: "Uncertain " + i,
            kind: "movie",
          })),
          total: n,
        });
      }
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
});

async function render() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
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
  for (let i = 0; i < 6; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

const reviewLink = () =>
  host.querySelector<HTMLAnchorElement>("a.app-shell__review");

describe("reaching Review", () => {
  // The reported bug, in one assertion.
  it("offers a way in when only a collision is waiting", async () => {
    mount({ review: 0, collisions: [{}] });
    await render();
    expect(reviewLink()).not.toBeNull();
  });

  it("still offers one when only the match queue has something", async () => {
    mount({ review: 3, collisions: [] });
    await render();
    expect(reviewLink()).not.toBeNull();
    expect(reviewLink()!.textContent).toContain("3");
  });

  // The badge is a count of things to look at, so it has to count both or it
  // sends somebody to a screen holding more than it admitted to.
  it("counts both queues together", async () => {
    mount({ review: 2, collisions: [{}, {}] });
    await render();
    expect(reviewLink()!.textContent).toContain("4");
  });

  /*
   * Answering the report is the whole of v0.8.29. A badge that keeps counting
   * what somebody has already accepted hands the nagging straight back, in the
   * one place they cannot dismiss it from.
   */
  it("stops counting a collision somebody has already accepted", async () => {
    mount({ review: 0, collisions: [{ dismissed: true }] });
    await render();
    expect(reviewLink()).toBeNull();
  });

  it("counts the open ones alongside the accepted ones", async () => {
    mount({ review: 0, collisions: [{ dismissed: true }, {}] });
    await render();
    expect(reviewLink()!.textContent).toContain("1");
  });

  /*
   * A member is never shown the report — it returns paths and the server
   * refuses it to anyone else. Badging it for them would advertise a screen
   * whose contents they cannot be given, and fire a 403 to discover that.
   */
  it("does not badge a member for a report they cannot open", async () => {
    mount({ review: 0, collisions: [{}], role: "member" });
    await render();
    expect(reviewLink()).toBeNull();
  });
});
