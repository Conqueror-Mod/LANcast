/*
 * The library row's actions, after they stopped being five buttons.
 *
 * Every library used to carry Edit, Scan, Refresh metadata, Re-read filenames
 * and Remove at equal weight, so a five-library server put twenty-five controls
 * on one pane. Scan keeps its button and the rest moved into an overflow menu.
 *
 * What is worth a test rather than a read is the part that has bitten this
 * screen before: a control rendered into a place nobody can reach is the same
 * failure settingsRules.test.tsx was written for, and moving four controls
 * behind a menu is exactly the change that produces it. So these assert the
 * actions are *reachable and wired*, not that the menu looks a certain way.
 *
 * The other half is feedback. Refresh metadata was the one action on this pane
 * that said nothing at all — it answers 202 and wakes the enrich worker, so the
 * only evidence it had run was the activity dot in the nav, which says the
 * server is busy but not with what and not for which library. A control whose
 * success is indistinguishable from doing nothing is the failure this project
 * keeps finding in its own UI.
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

const libraries = [
  {
    id: 7,
    name: "Films",
    kind: "movie",
    path: "D:/Media/Films",
    roots: [
      { id: 1, library_id: 7, path: "D:/Media/Films", created_at: 1, item_count: 380 },
    ],
    created_at: 1,
    scanned_at: 2,
    item_count: 380,
    media_count: 380,
  },
];

let host: HTMLDivElement;
let root: Root;
let writes: { url: string; method: string }[];

/** scanConflict makes POSTing a scan answer the way a busy server does. */
function mount(opts: { scanConflict?: boolean } = {}) {
  writes = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      const json = (v: unknown, status = 200) =>
        new Response(JSON.stringify(v), {
          status,
          headers: { "Content-Type": "application/json" },
        });
      if (method !== "GET") {
        writes.push({ url, method });
        if (url.includes("/reparse")) return json({ examined: 0, changed: 0 });
        if (url.endsWith("/api/libraries/scan")) {
          return json({
            started: [{ library_id: 7, state: "running", files_seen: 0 }],
            busy: [],
          });
        }
        if (url.endsWith("/scan") && opts.scanConflict) {
          // What the server really sends: the running scan's progress, with a
          // 409 and no error envelope at all. See docs/api.md.
          return json({ library_id: 7, state: "running", files_seen: 12 }, 409);
        }
        return json({});
      }
      if (url.includes("/api/auth/status")) {
        return json({
          authenticated: true,
          configured: true,
          user: { id: "u1", name: "chris", role: "admin" },
        });
      }
      if (url.includes("/scan")) return json({ state: "idle" });
      if (url.includes("/api/libraries")) return json(libraries);
      if (url.includes("/api/settings")) return json({});
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

async function settle() {
  for (let i = 0; i < 4; i++) {
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
        <MemoryRouter initialEntries={["/settings?pane=libraries"]}>
          <Settings />
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
  await settle();
}

function buttons(label: string): HTMLButtonElement[] {
  return [...host.querySelectorAll("button")].filter(
    (b) => b.textContent?.trim() === label,
  ) as HTMLButtonElement[];
}

function openRowMenu(): void {
  const trigger = [...host.querySelectorAll("button")].find(
    (b) => b.getAttribute("aria-haspopup") === "menu",
  );
  if (!trigger) throw new Error("no overflow menu on the library row");
  act(() => trigger.click());
}

function text(): string {
  return host.textContent ?? "";
}

describe("the library row's actions", () => {
  it("keeps Scan a button rather than burying it in the menu", async () => {
    mount();
    await render();
    // Before the menu is opened: scanning is what this pane is for, and it must
    // not have become a two-click action to tidy up the four around it.
    expect(buttons("Scan").length).toBe(1);
  });

  it("reaches every moved action through the menu", async () => {
    mount();
    await render();
    for (const label of ["Edit", "Re-read filenames", "Refresh metadata", "Remove"]) {
      expect(buttons(label).length, `${label} before opening`).toBe(0);
    }
    openRowMenu();
    for (const label of ["Edit", "Re-read filenames", "Refresh metadata", "Remove"]) {
      expect(buttons(label).length, `${label} after opening`).toBe(1);
    }
  });

  /*
   * Refresh and reparse take the same argument and sit next to each other, so a
   * menu item wired to the wrong one would still "work" — the same trap
   * reparseButton.test.tsx guards for the button it replaced.
   */
  it("refreshes metadata through the refresh endpoint, and says it did", async () => {
    mount();
    await render();
    openRowMenu();
    act(() => buttons("Refresh metadata")[0].click());
    await settle();

    expect(writes.some((w) => w.url.includes("/refresh"))).toBe(true);
    expect(writes.some((w) => w.url.includes("/reparse"))).toBe(false);
    // The wording promises what will happen, not what has: the request only
    // clears the stamps and wakes the worker.
    expect(text()).toContain("matched against its provider again");
  });

  it("says a scan was already running instead of failing silently", async () => {
    mount({ scanConflict: true });
    await render();
    act(() => buttons("Scan")[0].click());
    await settle();

    // The 409 body carries no error code or message, so parsed as an error it
    // reads "Conflict" and nothing else. Branching on the status is what turns
    // it back into a sentence.
    expect(text()).toContain("Already scanning");
    expect(text()).not.toContain("Conflict");
  });

  /*
   * docs/design.md calls a keyboard dead end a bug outright. The items are real
   * buttons, so Tab walks into them; when the menu unmounts underneath the
   * focused item, focus falls to <body> and the way back is tabbing from the
   * top of the pane.
   */
  it("puts focus back on the trigger when the menu closes", async () => {
    mount();
    await render();
    const trigger = [...host.querySelectorAll("button")].find(
      (b) => b.getAttribute("aria-haspopup") === "menu",
    ) as HTMLButtonElement;
    act(() => trigger.click());

    const item = buttons("Refresh metadata")[0];
    act(() => item.focus());
    expect(document.activeElement).toBe(item);

    act(() => {
      document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    });
    expect(document.activeElement).toBe(trigger);
  });

  it("closes the menu once an action is taken", async () => {
    mount();
    await render();
    openRowMenu();
    expect(buttons("Refresh metadata").length).toBe(1);
    act(() => buttons("Refresh metadata")[0].click());
    await settle();
    // A menu left standing over the answer it just produced hides the thing you
    // pressed it for.
    expect(buttons("Refresh metadata").length).toBe(0);
  });
});

describe("scanning every library", () => {
  it("posts the sweep and reports what it started", async () => {
    mount();
    await render();
    const all = buttons("Scan all")[0];
    expect(all, "no Scan all control on the libraries pane").toBeTruthy();
    act(() => all.click());
    await settle();

    const sweep = writes.find((w) => w.url.endsWith("/api/libraries/scan"));
    expect(sweep, "the sweep did not post to /api/libraries/scan").toBeTruthy();
    expect(sweep?.method).toBe("POST");
    // Saying how many started is the whole point: "nothing happened" and "all
    // of them were already scanning" are different answers.
    expect(text()).toContain("Scanning 1 library");
  });
});
