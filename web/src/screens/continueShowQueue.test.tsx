/*
 * Continue on a show hands over the rest of the show.
 *
 * It navigated with no queue at all, and the player falls back to a queue
 * holding only the item it was given. So resuming a show played exactly one
 * episode and stopped — and with repeat on, a one-item queue wraps onto itself
 * and the *same episode replayed for ever*, which is how it was found: "we just
 * finished watching an episode of Futurama and it replayed the same episode".
 *
 * Play from the top already did this correctly. Continue was the one path that
 * forgot, which is why the test asserts the queue rather than the destination —
 * the destination was always right, and that is what made it look fine.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  MemoryRouter,
  Routes,
  Route,
  useLocation,
  useParams,
} from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider } from "@/playback/PlaybackProvider";
import { Detail } from "./Detail";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const show = {
  id: 900,
  title: "Futurama",
  kind: "show",
  library_id: 1,
  child_count: 4,
  artwork: {},
};

function ep(id: number, n: number) {
  return {
    id,
    title: `Episode ${n}`,
    kind: "episode",
    library_id: 1,
    series: "Futurama",
    season: 1,
    episode: n,
    duration_ms: 1_353_087,
    artwork: {},
  };
}

// Four episodes; the viewer is part way through the third.
const episodes = [ep(101, 1), ep(102, 2), ep(103, 3), ep(104, 4)];

let host: HTMLDivElement;
let root: Root;
/** What the player screen was handed, captured from the route it landed on. */
let landed: { path: string; state: unknown } | null;

function mount() {
  landed = null;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/continue")) {
        return json({ episode: ep(103, 3), resume: true, exhausted: false });
      }
      if (url.includes("/episodes")) return json({ episodes });
      // Children arrive from /api/items?parent_id=, not a /children path.
      if (url.includes("parent_id=")) {
        return json({ items: episodes, total: episodes.length });
      }
      if (url.includes("/api/items/900")) return json(show);
      if (url.includes("/api/auth/status")) {
        return json({
          authenticated: true,
          configured: true,
          user: { id: "u1", name: "chris", role: "admin" },
        });
      }
      return json({ items: [], total: 0 });
    }),
  );
}

/** Stands in for the player, recording what it was navigated with. */
function FakePlayer() {
  const loc = useLocation();
  const params = useParams();
  landed = { path: params.id as string, state: loc.state };
  return <div>player</div>;
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
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <PlaybackProvider>
            <MemoryRouter initialEntries={["/item/900"]}>
              <Routes>
                <Route path="/item/:id" element={<Detail />} />
                <Route path="/watch/:id" element={<FakePlayer />} />
              </Routes>
            </MemoryRouter>
          </PlaybackProvider>
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

/*
 * Matched on the trailing word, because the label carries a play glyph — the
 * button reads "▶Continue", and both it and "Continue watching" call the
 * same handler.
 */
function buttonSaying(label: string): HTMLButtonElement | undefined {
  return [...host.querySelectorAll("button")].find(
    (b) => (b.textContent ?? "").trim().replace(/^▶/, "") === label,
  ) as HTMLButtonElement | undefined;
}

describe("continuing a show", () => {
  it("queues the rest of the show, not just the one episode", async () => {
    mount();
    await render();

    const cont = buttonSaying("Continue");
    expect(cont, "no Continue button on the show page").toBeTruthy();
    act(() => cont!.click());
    for (let i = 0; i < 6; i++) {
      await act(async () => {
        await new Promise((r) => setTimeout(r, 5));
      });
    }

    expect(landed, "never reached the player").toBeTruthy();
    expect(landed!.path).toBe("103");

    const queue = (landed!.state as { queue?: number[] })?.queue;
    /*
     * The assertion that matters. A single-item queue is what stranded the
     * show: nothing to advance to, and with repeat on it wraps onto itself.
     */
    expect(queue, "Continue handed over no queue at all").toBeTruthy();
    expect(queue!.length).toBeGreaterThan(1);
    // From here onward, not the whole show: Continue means carry on, and
    // queueing the earlier episodes would send `previous` back through ones
    // already finished.
    expect(queue).toEqual([103, 104]);
  });
});
