/*
 * A detail page survives arriving.
 *
 * This is the test whose absence let v0.6.48 ship a black screen. Two useState
 * calls were written below `if (isLoading) return`, so the first render
 * registered fewer hooks than the second, React refused to reconcile, and the
 * whole tree unmounted — every detail page, no error boundary, restart the app.
 *
 * The shape matters more than the assertions: the item must NOT be in the cache
 * when the component mounts, so there is a loading render followed by a loaded
 * one. Seeding the cache first would render the loaded state directly and pass
 * against the broken code, which is exactly how a film opened from the grid
 * hid this fault while a show exposed it.
 *
 * A show and a film are both covered because the fault was reported as
 * show-specific and was not — it was latent everywhere and only the timing
 * differed.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { Detail } from "./Detail";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider } from "@/playback/PlaybackProvider";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const show = {
  id: 7,
  library_id: 1,
  kind: "show",
  title: "A Show",
  sort_title: "a show",
  year: 2019,
  missing: false,
};

const movie = { ...show, id: 8, kind: "movie", title: "A Film", sort_title: "a film" };

let host: HTMLDivElement;
let root: Root;
let errors: string[];

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  errors = [];
  // React reports a failed reconciliation through console.error before the tree
  // goes; capturing it turns "the screen is blank" into a readable failure.
  vi.spyOn(console, "error").mockImplementation((...args: unknown[]) => {
    errors.push(args.map(String).join(" "));
  });
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function stubFetch(item: Record<string, unknown>) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const json = (body: unknown) =>
        new Response(JSON.stringify(body), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/children")) return json({ items: [] });
      if (/\/api\/items\/\d+$/.test(url.split("?")[0])) return json(item);
      if (url.includes("/api/items")) return json({ items: [], total: 0 });
      if (url.includes("/api/libraries")) return json([]);
      return json({});
    }),
  );
}

async function renderDetail(item: Record<string, unknown>) {
  stubFetch(item);
  // No seeded cache: the first render must be the loading one.
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        {/* The same providers App mounts: Detail registers a back handler with
            the focus controller, which is a context rather than a global. */}
        <FocusProvider>
          <PlaybackProvider>
            <MemoryRouter initialEntries={[`/item/${item.id}`]}>
              <Routes>
                <Route path="/item/:id" element={<Detail />} />
              </Routes>
            </MemoryRouter>
          </PlaybackProvider>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  // Let the fetch resolve, which is the render that used to crash.
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}

function hookOrderErrors(): string[] {
  return errors.filter(
    (e) => e.includes("more hooks") || e.includes("Rendered fewer hooks"),
  );
}

describe("a detail page arriving", () => {
  it("renders a show without changing its hook count", async () => {
    await renderDetail(show);

    expect(hookOrderErrors()).toEqual([]);
    // Still mounted and showing the thing, rather than an empty document.
    expect(host.textContent).toContain("A Show");
  });

  it("renders a film without changing its hook count", async () => {
    await renderDetail(movie);

    expect(hookOrderErrors()).toEqual([]);
    expect(host.textContent).toContain("A Film");
  });

  /*
   * The blank screen itself, asserted directly rather than through React's
   * error text: whatever else is wrong, the page must not come back empty.
   */
  it("is not blank after the item loads", async () => {
    await renderDetail(show);
    expect(host.querySelector(".detail")).not.toBeNull();
    expect((host.textContent ?? "").trim().length).toBeGreaterThan(0);
  });
});

/*
 * The rewatch count on a detail page.
 *
 * Shown from two, because "watched once" is what the tick already says and
 * repeating it in words on every finished title would be noise across most of a
 * library. The interesting assertions are the two boundaries and the case where
 * the flag and the count disagree — marking something unwatched keeps the
 * tally, so `watched: false` with a count of four is an ordinary state and not
 * a contradiction to be smoothed over.
 */
describe("the rewatch count", () => {
  it("says nothing for something watched once", async () => {
    await renderDetail({
      ...movie,
      progress: { position_ms: 0, watched: true, watch_count: 1 },
    });
    expect(host.textContent).not.toContain("times");
  });

  it("says how many times once there have been several", async () => {
    await renderDetail({
      ...movie,
      progress: { position_ms: 0, watched: true, watch_count: 4 },
    });
    expect(host.textContent).toContain("Watched 4 times");
  });

  it("keeps saying so after the title is marked unwatched", async () => {
    // The count outlives the flag on purpose: putting something back on the
    // list is not a claim never to have seen it.
    await renderDetail({
      ...movie,
      progress: { position_ms: 0, watched: false, watch_count: 4 },
    });
    expect(host.textContent).toContain("Watched 4 times");
  });

  /*
   * An older server does not send the field at all — the client updates
   * through the installer while the server updates itself, so a client ahead
   * of its server is ordinary. Absent must read as "no count", never as one.
   */
  it("says nothing when the server does not report a count", async () => {
    await renderDetail({
      ...movie,
      progress: { position_ms: 0, watched: true },
    });
    expect(host.textContent).not.toContain("times");
  });
});

/*
 * A title whose file has gone.
 *
 * Scanning marks missing rather than deleting — an unmounted drive must not
 * destroy library data — so these rows are ordinary and long-lived: one real
 * library holds 62 films and 747 photographs in this state. Every one of them
 * used to offer Play, and the only way to learn the file had gone was to press
 * it and watch the player fail with a message blaming server load.
 */
describe("a title whose file is missing", () => {
  it("does not offer to play it", async () => {
    await renderDetail({ ...movie, missing: true });
    // The button is the whole fault: pressing it produces a refusal the player
    // cannot render, which is why this is asserted rather than the message.
    expect(host.textContent).not.toContain("Play");
  });

  it("says why, rather than leaving an empty space where the button was", async () => {
    await renderDetail({ ...movie, missing: true });
    expect(host.textContent).toContain("not on the disk");
  });

  it("says the row is kept, because that is the rule and not an accident", async () => {
    // A library that quietly shrinks when a drive is unplugged is exactly what
    // marking missing exists to prevent, so the screen says so.
    await renderDetail({ ...movie, missing: true });
    expect(host.textContent).toContain("comes back");
  });

  it("still plays a title whose file is there", async () => {
    await renderDetail({ ...movie, missing: false });
    expect(host.textContent).toContain("Play");
    expect(host.textContent).not.toContain("not on the disk");
  });
});
