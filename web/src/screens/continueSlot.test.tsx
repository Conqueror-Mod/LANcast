/*
 * Which player may offer Continue (ADR 0077 part c).
 *
 * The browser's states and the desktop's are different formats, measured: the
 * browser's RetroArch writes RZIP-compressed states, the desktop stores a
 * core's raw serialization. They share the one `auto` slot, and whichever
 * player was used last owns it. So each offers Continue only from a state it
 * can load. A Continue that fails when pressed is the bug this guards.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider } from "@/playback/PlaybackProvider";
import { Detail } from "./Detail";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const game = {
  id: 77,
  library_id: 3,
  kind: "rom",
  title: "Bubble Bobble",
  sort_title: "bubble bobble",
  platform: "nes",
  missing: false,
  match_state: "matched",
};

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
  delete (window as { lancastRetroOpen?: unknown }).lancastRetroOpen;
  delete (window as { lancastRetroAvailable?: unknown }).lancastRetroAvailable;
});

function serve(autoCore: string) {
  vi.stubGlobal("matchMedia", () => ({ matches: false }));
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const json = (body: unknown) =>
        new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
      if (url.includes("/api/retro/web-cores")) {
        return json({
          cores: [{ name: "fceumm", display: "FCEUmm", licence: "GPL-2.0", source_url: "x", consoles: ["nes"], size_bytes: 1, installed: true }],
          archive: { url: "x", size_bytes: 1, version: "1.22.2" },
          job: { running: false, stage: "", done: 0, total: 0 },
        });
      }
      if (url.includes("/saves")) {
        return json({ saves: [{ slot: "auto", size_bytes: 10, updated_at: 1, core: autoCore, core_version: "1" }] });
      }
      if (url.includes("/children")) return json({ items: [] });
      if (/\/api\/items\/\d+$/.test(url.split("?")[0])) return json(game);
      if (url.includes("/api/items")) return json({ items: [], total: 0 });
      if (url.includes("/api/libraries")) return json([]);
      return json({});
    }),
  );
}

// The desktop app's bindings, playing NES with its own core.
function asDesktop() {
  (window as { lancastRetroOpen?: unknown }).lancastRetroOpen = vi.fn();
  (window as { lancastRetroAvailable?: unknown }).lancastRetroAvailable = vi.fn(async () => ({
    available: true,
    core: "mesen",
  }));
}

async function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <PlaybackProvider>
            <MemoryRouter initialEntries={[`/item/${game.id}`]}>
              <Routes>
                <Route path="/item/:id" element={<Detail />} />
              </Routes>
            </MemoryRouter>
          </PlaybackProvider>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  for (let i = 0; i < 10; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

const buttons = () => [...host.querySelectorAll("button")].map((b) => (b.textContent ?? "").trim());
const offersContinue = () => buttons().some((b) => b.endsWith("Continue"));

describe("Continue on a retro game's page", () => {
  it("is offered in a browser from the browser's own state", async () => {
    serve("retroarch-web/fceumm");
    await render();
    expect(offersContinue()).toBe(true);
  });

  it("is not offered in a browser from the desktop's state", async () => {
    serve("Mesen");
    await render();
    expect(offersContinue()).toBe(false);
    expect(buttons().some((b) => b.endsWith("Play"))).toBe(true);
  });

  it("is not offered on the desktop from a browser's state", async () => {
    asDesktop();
    serve("retroarch-web/fceumm");
    await render();
    expect(offersContinue()).toBe(false);
  });

  it("is still offered on the desktop from its own state", async () => {
    asDesktop();
    serve("Mesen");
    await render();
    expect(offersContinue()).toBe(true);
  });
});
