/*
 * The keyboard map is only real if the app obeys it.
 *
 * The first version of the customizer stored an override, rendered it back in
 * both the settings pane and the shortcut overlay, and changed nothing: the
 * search handler was a literal `e.key !== "/"` and the player was a switch on
 * hard-coded keys. Every unit test passed, because they all asked the store
 * what it held rather than asking the app what it did. A browser pass caught
 * it in about a minute.
 *
 * So this test presses keys at the real shell and asserts the navigation,
 * which is the only claim worth making: that a rebound key *works* and the
 * default it replaced stops working.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Routes, Route, useLocation } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { AppShell } from "./AppShell";
import { writeDevice } from "@/lib/device";
import { KEYS_STORAGE_KEY, type KeyOverrides } from "@/lib/keys";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}

let container: HTMLDivElement;
let root: Root;
let seen = "";

function Probe() {
  seen = useLocation().pathname;
  return null;
}

beforeEach(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  localStorage.clear();
  writeDevice<KeyOverrides>(KEYS_STORAGE_KEY, {});
  writeDevice<boolean>("lancast:bigscreen", false);
  seen = "";
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  // The shell fetches libraries, the review count and auth. None of that is
  // what is under test, so it answers empty rather than being mocked in detail.
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      new Response(JSON.stringify({ items: [], libraries: [], total: 0 }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    ),
  );
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  vi.unstubAllGlobals();
});

function render() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  act(() => {
    root.render(
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={["/"]}>
          <FocusProvider>
            <AppShell>
              <Routes>
                <Route path="*" element={<Probe />} />
              </Routes>
            </AppShell>
          </FocusProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
}

function press(key: string) {
  act(() => {
    document.dispatchEvent(
      new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true }),
    );
  });
}

describe("shortcuts are wired to the bindings, not to literals", () => {
  it("opens search on the default key", () => {
    render();
    press("/");
    expect(seen).toBe("/search");
  });

  it("opens search on a rebound key", () => {
    writeDevice<KeyOverrides>(KEYS_STORAGE_KEY, { search: ["s"] });
    render();
    press("s");
    expect(seen).toBe("/search");
  });

  // The half that the original bug left working, and which made the bug hard
  // to see: the old key kept doing the job it had been rebound away from.
  it("stops answering the default once it has been rebound", () => {
    writeDevice<KeyOverrides>(KEYS_STORAGE_KEY, { search: ["s"] });
    render();
    press("/");
    expect(seen).toBe("/");
  });
});
