/*
 * The people page, and the three empty screens it must keep apart.
 *
 * A grid with nothing in it means one of: the face worker is not installed,
 * nothing has looked at this library yet, or it has looked and found nobody.
 * They are the same empty grid and three completely different sentences, and
 * telling somebody the wrong one is how a working feature gets reported as
 * broken.
 *
 * jsdom paints nothing, so these are about which sentence is shown and what is
 * asked of the server — not about how the faces look.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { FacePeople } from "./FacePeople";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

type Scenario = {
  ready: boolean;
  reason?: string;
  people?: unknown[];
  pending?: number;
};

let host: HTMLDivElement;
let root: Root;
let gets: string[];
let sent: { url: string; body: string }[];

function mount(s: Scenario) {
  gets = [];
  sent = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (method !== "GET") {
        sent.push({ url, body: String(init?.body ?? "") });
        return json({ ok: true });
      }
      gets.push(url);
      if (url.includes("/api/auth/status")) {
        return json({
          authenticated: true,
          configured: true,
          user: { id: "u1", name: "chris", role: "admin" },
        });
      }
      if (url.includes("/api/faces/capabilities")) {
        return json({ ready: s.ready, reason: s.reason });
      }
      if (url.includes("/people")) {
        return json({ people: s.people ?? [], pending: s.pending ?? 0 });
      }
      if (url.includes("/api/libraries")) {
        return json([{ id: 5, name: "Pictures", kind: "picture" }]);
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
        <FocusProvider>
          <MemoryRouter initialEntries={["/library/5/people"]}>
            <Routes>
              <Route path="/library/:id/people" element={<FacePeople />} />
            </Routes>
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

describe("the three empty people pages", () => {
  it("says so when the worker is not installed, and why", async () => {
    mount({ ready: false, reason: "the face worker is not installed" });
    await render();
    expect(host.textContent).toContain("not set up");
    expect(host.textContent).toContain("not installed");
    // And does not invite somebody to press a button that cannot work.
    expect(host.textContent).not.toContain("Find faces");
  });

  it("offers to look when it can but has not", async () => {
    mount({ ready: true, people: [], pending: 0 });
    await render();
    expect(host.textContent).toContain("Find faces");
    expect(host.textContent).toContain("No faces yet");
  });

  it("says it is still looking rather than that nobody is there", async () => {
    mount({ ready: true, people: [], pending: 1204 });
    await render();
    expect(host.textContent).toContain("Still looking");
    expect(host.textContent).toContain("1,204");
    expect(host.textContent).not.toContain("No faces yet");
  });

  /*
   * It has to say *when* people appear, because it used to say the wrong
   * thing.
   *
   * "Groups appear as they are found" is not what happens: grouping runs once,
   * after every photograph has been examined. Reported from a real library —
   * 2,810 faces found, no groups, and the only reading available was that the
   * search had stopped, so it was pressed again. The work was fine and the
   * sentence was wrong, which is the worst combination because nothing fails.
   */
  it("says the page stays empty until the search finishes", async () => {
    mount({ ready: true, people: [], pending: 1204 });
    await render();
    expect(host.textContent).not.toContain("Groups appear as they are found");
    expect(host.textContent).toMatch(/once every photograph has been looked at/i);
  });
});

describe("the grid", () => {
  const people = [
    { id: 1, name: "Georgia", name_locked: true, count: 41, cover_face_id: 9 },
    { id: 2, name: null, name_locked: false, count: 88, cover_face_id: 10 },
    { id: 3, name: null, name_locked: false, count: 4, cover_face_id: 11 },
  ];

  it("shows a name where there is one and a prompt where there is not", async () => {
    mount({ ready: true, people });
    await render();
    expect(host.textContent).toContain("Georgia");
    expect(host.textContent).toContain("Who is this?");
  });

  /*
   * The page's job is to get faces named, so the most valuable thing on it is
   * always the biggest group nobody has identified. Sorting named people first
   * would bury the work under the finished part of it.
   */
  it("puts the largest unnamed group first", async () => {
    mount({ ready: true, people });
    await render();
    const labels = [...host.querySelectorAll(".faceperson__count")].map(
      (n) => n.textContent,
    );
    expect(labels[0]).toBe("88");
    expect(labels[labels.length - 1]).toBe("41");
  });

  // Each face is drawn from the server's own crop, so a client never
  // reimplements the cropping rules.
  it("draws faces from the thumbnail endpoint", async () => {
    mount({ ready: true, people });
    await render();
    const srcs = [...host.querySelectorAll("img")].map((i) =>
      i.getAttribute("src"),
    );
    expect(srcs.some((s) => s?.includes("/api/faces/9/thumb"))).toBe(true);
  });
});

describe("naming", () => {
  const people = [
    { id: 7, name: null, name_locked: false, count: 12, cover_face_id: 21 },
  ];

  it("sends the typed name for that group", async () => {
    mount({ ready: true, people });
    await render();

    const tile = host.querySelector(".faceperson") as HTMLButtonElement;
    await act(async () => {
      tile.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    const input = host.querySelector(".facenamer__field input") as HTMLInputElement;
    expect(input, "the naming panel did not open").not.toBeNull();

    const setter = Object.getOwnPropertyDescriptor(
      window.HTMLInputElement.prototype,
      "value",
    )!.set!;
    await act(async () => {
      setter.call(input, "Georgia");
      input.dispatchEvent(new Event("input", { bubbles: true }));
    });
    const save = [...host.querySelectorAll("button")].find(
      (b) => b.textContent === "Save",
    )!;
    await act(async () => {
      save.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    for (let i = 0; i < 4; i++) {
      await act(async () => {
        await new Promise((r) => setTimeout(r, 5));
      });
    }

    const put = sent.find((s) => s.url.includes("/api/faces/clusters/7"));
    expect(put, `no name was sent; requests: ${JSON.stringify(sent)}`).toBeTruthy();
    expect(put!.body).toContain("Georgia");
  });

  /*
   * The promise that makes naming safe to do is written on the panel.
   *
   * Somebody who believes a re-run might undo their work will not do the work,
   * and this is the one screen where that belief would form.
   */
  it("promises that a name survives re-grouping", async () => {
    mount({ ready: true, people });
    await render();
    const tile = host.querySelector(".faceperson") as HTMLButtonElement;
    await act(async () => {
      tile.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    expect(host.textContent).toContain("never rename");
  });
});
