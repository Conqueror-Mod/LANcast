/*
 * A film in a collection, resumed, carries on through the collection.
 *
 * Reported: a collection resumed from Continue Watching played the one film
 * and stopped. The shelf opens a film's page, and that page's Continue
 * watching handed the player one id. The spotlight already queued the
 * franchise when resuming (HomeHero); the page did not.
 *
 * Pressing Play on a film nobody has started still plays it alone — that is
 * not a request for its box set — unless the page was opened from Continue
 * Watching as the collection's next film.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Routes, Route, useLocation, useParams } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider } from "@/playback/PlaybackProvider";
import { Detail } from "./Detail";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const film = (id: number, progress?: { position_ms: number; watched: boolean }) => ({
  id, title: `Film ${id}`, kind: "movie", library_id: 1, duration_ms: 6_000_000,
  container: "matroska", artwork: {}, progress,
});

let host: HTMLDivElement;
let root: Root;
let landed: { path: string; state: unknown } | null;

function stub(item: Record<string, unknown>) {
  landed = null;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), { status: 200, headers: { "Content-Type": "application/json" } });
      const path = url.split("?")[0];
      if (path.endsWith("/collections")) {
        return json({ items: [{ id: 50, kind: "collection", title: "Saga", library_id: 1 }] });
      }
      if (url.includes("collection_id=50")) {
        // Release order: 699 watched, 700 the one on this page, 701 and 702 ahead.
        return json({
          items: [film(699, { position_ms: 0, watched: true }), film(700), film(701), film(702)],
          total: 4,
        });
      }
      if (/\/api\/items\/\d+$/.test(path)) return json(item);
      if (url.includes("/api/auth/status")) {
        return json({ authenticated: true, configured: true, user: { id: "u1", role: "member" } });
      }
      return json({ items: [], total: 0 });
    }),
  );
}

function FakePlayer() {
  const loc = useLocation();
  const params = useParams();
  landed = { path: params.id as string, state: loc.state };
  return <div>player</div>;
}

async function settle() {
  for (let i = 0; i < 8; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

async function render(state?: unknown) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <PlaybackProvider>
            <MemoryRouter initialEntries={[{ pathname: "/item/700", state }]}>
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
  await settle();
}

const button = (label: string) =>
  [...host.querySelectorAll("button")].find(
    (b) => (b.textContent ?? "").trim().replace(/^▶/, "") === label,
  ) as HTMLButtonElement | undefined;

async function press(label: string) {
  const b = button(label);
  expect(b, `no "${label}" button`).toBeTruthy();
  await act(async () => b!.click());
  await settle();
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

describe("resuming a film in a collection", () => {
  it("queues the rest of the collection after it", async () => {
    stub(film(700, { position_ms: 1_200_000, watched: false }));
    await render();
    await press("Continue watching");
    expect(landed?.path).toBe("700");
    expect(landed?.state).toEqual({ queue: [700, 701, 702] });
  });

  it("plays a film nobody has started on its own", async () => {
    stub(film(700));
    await render();
    await press("Play");
    expect(landed?.path).toBe("700");
    expect(landed?.state ?? null).toBeNull();
  });

  it("carries on when Continue Watching opened it as the collection's next film", async () => {
    stub(film(700));
    await render({ collectionID: 50 });
    await press("Play");
    expect(landed?.state).toEqual({ queue: [700, 701, 702] });
  });
});
