/*
 * The sharing toggle shows the state the server actually holds.
 *
 * Written because it did not. `/api/people` excludes the caller by design — a
 * row for yourself in a list of other people is noise — and the toggle looked
 * for itself in that list anyway. It never found itself, fell through to
 * `false`, and rendered unticked on every mount however long ago you had opted
 * in. Local state hid it until you left the pane and came back.
 *
 * The server-side test for this asserted the database after the PUT, which
 * passed the whole time the thing a person could see said the opposite. So this
 * one asserts the checkbox, from a server that says sharing is on.
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

function mockServer(sharing: boolean) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/api/auth/status")) {
        return json({
          configured: true,
          authenticated: true,
          lan_enabled: false,
          restart_required: false,
          user: { id: "local", name: "Conqueror", role: "admin", sharing },
        });
      }
      return json({});
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
        <MemoryRouter initialEntries={["/settings?pane=account"]}>
          <Settings />
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
}

/*
 * Wait for a condition rather than ticking a fixed number of times.
 *
 * The first version of this flushed microtasks twice, which was enough when the
 * file ran alone and not enough under a loaded suite — so it passed in isolation
 * and failed in CI. The half that made it hard to see is that only the "ticked"
 * case can fail that way: `false` is the default, so an assertion that runs
 * before the query resolves *agrees with* the unticked test for the wrong
 * reason.
 */
async function waitFor(cond: () => boolean, what: string) {
  for (let i = 0; i < 100; i++) {
    if (cond()) return;
    await act(async () => {
      await new Promise((r) => setTimeout(r, 10));
    });
  }
  throw new Error(`timed out waiting for ${what}`);
}

function sharingBox(): HTMLInputElement {
  const box = host.querySelector<HTMLInputElement>(
    ".set-toggle--described input[type=checkbox]",
  );
  if (!box) throw new Error("no sharing checkbox on the account pane");
  return box;
}

describe("the sharing toggle", () => {
  it("is ticked when the server says this account shares", async () => {
    mockServer(true);
    await render();
    await waitFor(() => sharingBox().checked, "the toggle to reflect sharing=true");
    expect(sharingBox().checked).toBe(true);
  });

  it("is unticked when the server says it does not", async () => {
    mockServer(false);
    await render();
    // Settle first, so this cannot pass merely by being asserted early —
    // `false` is also the pre-resolution value.
    await waitFor(
      () => host.querySelector(".set-toggle--described") !== null,
      "the account pane to render",
    );
    await act(async () => {
      await new Promise((r) => setTimeout(r, 50));
    });
    expect(sharingBox().checked).toBe(false);
  });
});
