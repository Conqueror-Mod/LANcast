/*
 * Deleting a playlist from its own page (R15-5).
 *
 * The page confirms by relabelling its button, and the second press deletes.
 * Nothing stopped a third press while that DELETE was in flight, which sent a
 * second one. TanStack runs a call's own onSuccess for the latest mutate only,
 * so the first (successful) delete did not go back, and the second got a 404
 * and did not either: the person was left on a playlist that no longer
 * existed. Presses while the delete is pending are ignored now.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Routes, Route } from "react-router";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider } from "@/playback/PlaybackProvider";
import { Detail } from "./Detail";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const playlist = {
  id: 31, title: "Road Trip", kind: "playlist", library_id: 3, child_count: 2, artwork: {},
};

let host: HTMLDivElement;
let root: Root;
let deletes: number;
let gone: boolean;
/** Holds the first DELETE open until the test lets it answer. */
let answerDelete: (() => void) | null;

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  deletes = 0;
  gone = false;
  answerDelete = null;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const json = (v: unknown, status = 200) =>
        new Response(JSON.stringify(v), { status, headers: { "Content-Type": "application/json" } });
      if (init?.method === "DELETE") {
        deletes++;
        if (gone) return json({ error: { code: "not_found", message: "no such playlist" } }, 404);
        await new Promise<void>((r) => (answerDelete = r));
        gone = true;
        return new Response(null, { status: 204 });
      }
      if (/\/api\/items\/31$/.test(url.split("?")[0])) return json(playlist);
      if (url.includes("/api/auth/status")) {
        return json({ authenticated: true, configured: true, user: { id: "u1", role: "admin" } });
      }
      return json({ items: [], total: 0 });
    }),
  );
});

afterEach(() => {
  answerDelete?.();
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function settle() {
  for (let i = 0; i < 6; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

async function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <PlaybackProvider>
            <MemoryRouter initialEntries={["/library/3/playlists", "/item/31"]} initialIndex={1}>
              <Routes>
                <Route path="/item/:id" element={<Detail />} />
                <Route path="/library/:id/playlists" element={<div id="playlists" />} />
              </Routes>
            </MemoryRouter>
          </PlaybackProvider>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await settle();
}

const deleteButton = () =>
  [...host.querySelectorAll<HTMLButtonElement>("button.detail__remove")][0];

describe("deleting a playlist from its page", () => {
  it("sends one DELETE however often it is pressed, and goes back", async () => {
    await render();
    await act(async () => deleteButton().click()); // "Delete playlist" → confirm
    expect(deleteButton().textContent).toBe("Delete this playlist?");
    // The confirming press, and another inside the same tick.
    await act(async () => {
      deleteButton().click();
      deleteButton().click();
    });
    await settle();
    // And one more once the page shows it is deleting.
    expect(deleteButton().textContent).toBe("Deleting…");
    expect(deleteButton().disabled).toBe(true);
    await act(async () => deleteButton().click());
    await settle();
    expect(deletes).toBe(1);

    await act(async () => answerDelete?.());
    await settle();
    expect(host.querySelector("#playlists")).not.toBeNull();
  });
});
