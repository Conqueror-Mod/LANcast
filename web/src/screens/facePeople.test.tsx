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
import { MemoryRouter, Routes, Route, useLocation } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import type { FacePerson } from "@/api/hooks";
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
  suggestions?: unknown[];
  faces?: unknown[];
};

/*
 * Where the router went.
 *
 * MemoryRouter keeps its own history and never touches window.location, so a
 * test that read window.location would pass whatever the app did. This records
 * the destination from a route the app actually renders.
 */
let landed = "";
function LocationProbe() {
  landed = useLocation().search;
  return <div data-testid="landed" />;
}
function lastSearch() {
  return landed;
}

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
      if (url.includes("/suggestions")) {
        return json({ people: s.suggestions ?? [] });
      }
      // After /suggestions, deliberately: every faces URL contains "/faces",
      // including "/api/faces/clusters/1/suggestions".
      if (/\/clusters\/\d+\/faces$/.test(url)) {
        return json({ faces: s.faces ?? [] });
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
  landed = "";
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
              <Route path="/library/:id" element={<LocationProbe />} />
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

/*
 * A group of one is held back until asked for.
 *
 * Measured on a real library: 126 of 343 groups hold a single face — 37% of
 * the groups and 2.7% of the faces. Every tile is the same size, so that
 * minority filled as much of the page as the groups of 301 and 222 that are
 * the reason to be on it, and the page read as mostly noise.
 */
describe("groups of one", () => {
  const many = [
    { id: 1, name: null, count: 301, cover_face_id: 11 },
    { id: 2, name: null, count: 1, cover_face_id: 12 },
    { id: 3, name: null, count: 1, cover_face_id: 13 },
  ] as unknown as FacePerson[];

  it("keeps them out of the grid until asked", async () => {
    mount({ ready: true, people: many, pending: 0 });
    await render();
    // The offer names how many, because 126 and 2 are different decisions.
    expect(host.textContent).toMatch(/Show 2 faces that matched nobody else/i);
  });

  it("shows them when asked", async () => {
    mount({ ready: true, people: many, pending: 0 });
    await render();
    const button = [...host.querySelectorAll("button")].find((b) =>
      /matched nobody else/i.test(b.textContent ?? ""),
    );
    if (!button) throw new Error("no toggle");
    await act(async () => {
      button.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    expect(host.textContent).toMatch(/Hide 2 faces that matched nobody else/i);
  });

  it("says nothing when every group has company", async () => {
    mount({
      ready: true,
      pending: 0,
      people: [{ id: 1, name: null, count: 12, cover_face_id: 11 }] as unknown as FacePerson[],
    });
    await render();
    expect(host.textContent).not.toMatch(/matched nobody else/i);
  });
});

/*
 * Accepting a suggestion has to look like it did something.
 *
 * Reported as "none of the additional matching faces can be selected". They
 * could: the click named the group server-side and returned. What did not
 * happen was anything visible — the suggestion list was never invalidated, so
 * the tile stayed exactly where it was, and a button that does the work and
 * changes nothing is indistinguishable from one that is broken.
 *
 * The rule CLAUDE.md states, on a list that did not exist until yesterday: a
 * write that changes what a list holds must invalidate that list.
 */
describe("accepting a suggested match", () => {
  it("marks the face as taken straight away", async () => {
    mount({
      ready: true,
      pending: 0,
      people: [{ id: 1, name: null, count: 40, cover_face_id: 11 }] as unknown as FacePerson[],
      suggestions: [{ id: 2, name: null, count: 1, cover_face_id: 22 }],
    });
    await render();

    // Open the naming panel and give a name.
    const tile = host.querySelector<HTMLElement>(".faceperson");
    if (!tile) throw new Error("no person tile");
    await act(async () => {
      tile.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    const input = host.querySelector(".facenamer__field input") as HTMLInputElement | null;
    if (!input) throw new Error("no name field");
    await act(async () => {
      const setter = Object.getOwnPropertyDescriptor(
        window.HTMLInputElement.prototype,
        "value",
      )!.set!;
      setter.call(input, "Georgia");
      input.dispatchEvent(new Event("input", { bubbles: true }));
    });
    const save = [...host.querySelectorAll("button")].find(
      (b) => b.textContent === "Save",
    );
    if (!save) throw new Error("no save button");
    await act(async () => {
      save.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    // The suggestions are only asked for once a name exists, so they arrive a
    // tick after the save resolves.
    await act(async () => {
      await new Promise((r) => setTimeout(r, 30));
    });

    const suggestion = host.querySelector<HTMLButtonElement>(
      ".facenamer__suggestion",
    );
    if (!suggestion) throw new Error("no suggestion offered");
    expect(suggestion.dataset.taken).toBeUndefined();

    await act(async () => {
      suggestion.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    const after = host.querySelector<HTMLButtonElement>(
      ".facenamer__suggestion",
    );
    // Either it is gone (the refetch landed) or it is marked. Both are
    // feedback; neither is the tile sitting there unchanged.
    if (after) {
      expect(after.dataset.taken).toBe("true");
    }
  });
});

/*
 * A person named across several groups is one tile.
 *
 * Reported: "Georgia Bowles" three times, at 80, 1 and 1 — the result of
 * accepting near-miss suggestions, which names a group rather than merging it.
 * The page read as three people who happened to share a name.
 */
describe("a person spread across groups", () => {
  const split = [
    { id: 1, name: "Georgia Bowles", count: 80, cover_face_id: 11 },
    { id: 2, name: "Georgia Bowles", count: 1, cover_face_id: 22 },
    { id: 3, name: "Georgia Bowles", count: 1, cover_face_id: 33 },
  ] as unknown as FacePerson[];

  it("appears once, with every face counted", async () => {
    mount({ ready: true, people: split, pending: 0 });
    await render();
    const names = [...host.querySelectorAll(".faceperson__name")].map(
      (n) => n.textContent,
    );
    expect(names.filter((n) => n === "Georgia Bowles")).toHaveLength(1);

    const counts = [...host.querySelectorAll(".faceperson__count")].map(
      (n) => n.textContent,
    );
    expect(counts).toContain("82");
  });

  /*
   * Renaming has to reach every group. Renaming one of three would split her
   * back into two people — the fault collapsing exists to fix, by another
   * route.
   */
  it("renames every group behind that one tile", async () => {
    mount({ ready: true, people: split, pending: 0 });
    await render();
    const tile = host.querySelector(".faceperson") as HTMLButtonElement;
    await act(async () => {
      tile.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    const input = host.querySelector(
      ".facenamer__field input",
    ) as HTMLInputElement;
    await act(async () => {
      const setter = Object.getOwnPropertyDescriptor(
        window.HTMLInputElement.prototype,
        "value",
      )!.set!;
      setter.call(input, "Georgia B");
      input.dispatchEvent(new Event("input", { bubbles: true }));
    });
    const save = [...host.querySelectorAll("button")].find(
      (b) => b.textContent === "Save",
    )!;
    await act(async () => {
      save.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    const named = sent.filter((r) => r.body.includes("Georgia B"));
    expect(named).toHaveLength(3);
    for (const id of [1, 2, 3]) {
      expect(named.some((r) => r.url.includes(`/clusters/${id}`))).toBe(true);
    }
  });
});

/*
 * Saying no.
 *
 * Until now every control on this panel was an "yes": name this group, accept
 * this near-miss. Grouping is sometimes wrong — reported as dozens of faces of
 * one person appearing under somebody else's name — and a screen that can only
 * agree with the machine leaves the person looking at it with nothing to do
 * about that.
 *
 * jsdom cannot see that the control is on the face rather than beside it. What
 * these check is that a refusal is sent, to the right place, for the right
 * thing.
 */
describe("removing a face from a person", () => {
  const scenario = {
    ready: true,
    pending: 0,
    people: [
      { id: 7, name: "Chris Bowles", name_locked: true, count: 40, cover_face_id: 11 },
    ] as unknown as FacePerson[],
    faces: [
      { id: 501, item_id: 1, score: 0.9 },
      { id: 502, item_id: 2, score: 0.8 },
    ],
  };

  it("deletes that face from that group and nothing else", async () => {
    mount(scenario);
    await render();

    const tile = host.querySelector<HTMLElement>(".faceperson");
    if (!tile) throw new Error("no person tile");
    await act(async () => {
      tile.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    await act(async () => {
      await new Promise((r) => setTimeout(r, 20));
    });

    const remove = host.querySelector<HTMLButtonElement>(".facenamer__remove");
    expect(remove, "no way to take a face out of a group").not.toBeNull();
    await act(async () => {
      remove!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    await act(async () => {
      await new Promise((r) => setTimeout(r, 20));
    });

    const del = sent.find((s) => s.url.includes("/api/faces/clusters/7/faces/501"));
    expect(
      del,
      `no rejection was sent; requests: ${JSON.stringify(sent)}`,
    ).toBeTruthy();
  });

  /*
   * The press has to land before the refetch answers.
   *
   * This is the same fault as the suggestion tiles, which were reported as
   * unclickable while working perfectly: a control that does the work and
   * changes nothing visible is indistinguishable from a broken one.
   */
  it("marks the face as gone straight away", async () => {
    mount(scenario);
    await render();
    const tile = host.querySelector<HTMLElement>(".faceperson");
    await act(async () => {
      tile!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    await act(async () => {
      await new Promise((r) => setTimeout(r, 20));
    });
    const remove = host.querySelector<HTMLButtonElement>(".facenamer__remove");
    await act(async () => {
      remove!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    const face = host.querySelector(".facenamer__face");
    expect(face?.getAttribute("data-gone")).toBe("true");
  });
});

/*
 * A suggestion you can only accept is a list that never gets shorter.
 *
 * The near-misses that are genuinely somebody else are exactly the ones that
 * stay near, so they come back on every visit. Dismissing is recorded on the
 * server, which is what makes the answer stick.
 */
describe("dismissing a suggested match", () => {
  it("sends the refusal against the named group", async () => {
    mount({
      ready: true,
      pending: 0,
      people: [
        { id: 1, name: null, count: 40, cover_face_id: 11 },
      ] as unknown as FacePerson[],
      suggestions: [{ id: 2, name: null, count: 1, cover_face_id: 22 }],
    });
    await render();

    const tile = host.querySelector<HTMLElement>(".faceperson");
    await act(async () => {
      tile!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    const input = host.querySelector(".facenamer__field input") as HTMLInputElement;
    await act(async () => {
      const setter = Object.getOwnPropertyDescriptor(
        window.HTMLInputElement.prototype,
        "value",
      )!.set!;
      setter.call(input, "Georgia");
      input.dispatchEvent(new Event("input", { bubbles: true }));
    });
    const save = [...host.querySelectorAll("button")].find(
      (b) => b.textContent === "Save",
    );
    await act(async () => {
      save!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    await act(async () => {
      await new Promise((r) => setTimeout(r, 30));
    });

    const dismiss = host.querySelector<HTMLButtonElement>(".facenamer__dismiss");
    expect(dismiss, "no way to say a suggestion is wrong").not.toBeNull();
    await act(async () => {
      dismiss!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    await act(async () => {
      await new Promise((r) => setTimeout(r, 20));
    });

    const del = sent.find((s) =>
      s.url.includes("/api/faces/clusters/1/suggestions/2"),
    );
    expect(
      del,
      `no dismissal was sent; requests: ${JSON.stringify(sent)}`,
    ).toBeTruthy();
    // And it is a refusal, not a rename: nothing may name the suggested group.
    const named = sent.find((s) => s.url.endsWith("/api/faces/clusters/2"));
    expect(named, "dismissing a suggestion named it instead").toBeFalsy();
  });
});

/*
 * The way to a person's photographs.
 *
 * This is the whole point of naming — the screen's own first paragraph says a
 * named group is how somebody finds a photograph — and until the face_cluster
 * filter existed it led nowhere.
 *
 * It is offered from the name panel rather than from the tile, and that was not
 * the first design. Making the tile navigate took away renaming, clearing and
 * telling it who somebody is not, all of which live behind that same tile — and
 * the tests below for removing a face went red saying so.
 *
 * The property worth pinning is that *every* group goes into the link. Naming
 * does not merge groups, so a person is routinely two or three, and passing
 * only the largest shows most of somebody's photographs while looking like a
 * complete answer. On a real library that was 277 of 350.
 */
describe("opening a person's photographs", () => {
  const twoGroups: FacePerson[] = [
    { id: 6, name: "Georgia", name_locked: true, count: 277, cover_face_id: 1 },
    { id: 51, name: "Georgia", name_locked: true, count: 73, cover_face_id: 2 },
  ];

  it("links to every group the person was collapsed from", async () => {
    mount({ ready: true, people: twoGroups, faces: [{ id: 1, item_id: 9, score: 0.9 }] });
    await render();

    // Open the person, the way somebody would.
    const tile = [...host.querySelectorAll("button")].find((b) =>
      (b.getAttribute("aria-label") ?? "").startsWith("Georgia"),
    );
    if (!tile) throw new Error("no tile for the named person");
    await act(async () => {
      tile.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    const view = [...host.querySelectorAll("button")].find(
      (b) => (b.textContent ?? "").trim() === "View photographs",
    );
    expect(view, "a named person should offer a way to their photographs").toBeTruthy();

    await act(async () => {
      view!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    // MemoryRouter keeps its own history, so the destination is read from what
    // the app rendered next rather than from window.location.
    const q = new URLSearchParams(lastSearch());
    expect(q.getAll("face_cluster").sort()).toEqual(["51", "6"].sort());
  });

  it("offers nothing of the kind for an unnamed group", async () => {
    // There is nothing to find yet: an unnamed group is a curiosity, and the
    // useful action is still naming it.
    mount({
      ready: true,
      people: [{ id: 9, name: null, name_locked: false, count: 4, cover_face_id: 3 }],
      faces: [{ id: 3, item_id: 9, score: 0.9 }],
    });
    await render();

    const tile = [...host.querySelectorAll("button")].find((b) =>
      (b.getAttribute("aria-label") ?? "") === "Unnamed person",
    );
    await act(async () => {
      tile!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    const view = [...host.querySelectorAll("button")].find(
      (b) => (b.textContent ?? "").trim() === "View photographs",
    );
    expect(view).toBeUndefined();
  });
});
