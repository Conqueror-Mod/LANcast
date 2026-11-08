import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { FriendsPanel, MemoryPanel, finishedVerb, memoryTiles, showFriends, yearsAgo } from "./BridgePanels";
import { friendRows } from "@/lib/presence";
import type { Item, PeerPresence } from "@/api/types";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const peer = (over: Partial<PeerPresence>): PeerPresence =>
  ({ fingerprint: "fp1", name: "Aither", state: "mutual", reachable: true, people: [], ...over }) as PeerPresence;

const georgia = { id: "u_9", name: "Georgia", granted: true, shares: true, online: true, watching: "Arrival" };
const quiet = { id: "u_2", name: "Quinn", granted: true, shares: true, online: true, watching: "" };
const hidden = { id: "u_3", name: "Hana", granted: true, shares: false };

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
});

function draw(el: React.ReactNode) {
  act(() =>
    root.render(
      <FocusProvider>
        <MemoryRouter>{el}</MemoryRouter>
      </FocusProvider>,
    ),
  );
}

describe("the friends panel's rows", () => {
  it("puts somebody you could join first, and a dead server apart", () => {
    const { rows, unreachable } = friendRows([
      peer({ people: [hidden, quiet, georgia] }),
      peer({ fingerprint: "fp2", name: "Kiousu", reachable: false, people: [quiet] }),
    ]);
    expect(rows.map((r) => r.person.name)).toEqual(["Georgia", "Quinn", "Hana"]);
    expect(rows[0].joinable).toBe(true);
    expect(rows[2].status).toBe("Not showing you what they watch");
    expect(unreachable.map((p) => p.name)).toEqual(["Kiousu"]);
  });

  it("keeps a server that only reaches us, whose people are known", () => {
    const { rows, unreachable } = friendRows([
      peer({ reachable: false, reaches_us: true, people: [georgia] }),
    ]);
    expect(unreachable).toEqual([]);
    expect(rows[0].status).toBe("What they watch can't be seen from here");
    // What they watch cannot be asked, so there is nothing to join.
    expect(rows[0].joinable).toBe(false);
  });
});

describe("the friends panel", () => {
  it("is offered with no pairings only to somebody who can pair", () => {
    expect(showFriends([], true)).toBe(true);
    expect(showFriends([], false)).toBe(false);
    expect(showFriends(undefined, true)).toBe(false);
    draw(<FriendsPanel peers={[]} />);
    expect(host.querySelector("a")?.getAttribute("href")).toBe("/settings?pane=peers");
  });

  it("offers Ask to join beside somebody watching, and nowhere else", () => {
    draw(<FriendsPanel peers={[peer({ people: [georgia, quiet] })]} />);
    expect(host.textContent).toContain("Watching Arrival");
    expect(host.textContent).not.toContain("Nobody's watching");
    expect(host.querySelectorAll("button.ask-join").length).toBe(1);
  });

  it("says nobody is watching, and names a server it cannot reach", () => {
    draw(
      <FriendsPanel
        peers={[peer({ people: [quiet] }), peer({ fingerprint: "fp2", name: "Kiousu", reachable: false })]}
      />,
    );
    expect(host.textContent).toContain("Nobody's watching right now.");
    expect(host.textContent).toContain("Kiousu");
    expect(host.textContent).toContain("Can't reach this server");
  });
});

describe("On this day", () => {
  const photo = (id: number, year: number) =>
    ({ id, kind: "photo", title: `IMG_${id}`, artwork: {}, taken_at: new Date(year, 10, 8).getTime() / 1000 }) as Item;

  it("counts local calendar years", () => {
    expect(yearsAgo(new Date(2023, 0, 1, 0, 30).getTime() / 1000, new Date(2026, 10, 8))).toBe(3);
  });

  it("names the year only when every photograph shown shares it", () => {
    const now = new Date().getFullYear();
    draw(<MemoryPanel items={[photo(1, now - 3), photo(2, now - 3)]} />);
    expect(host.textContent).toContain("3 years ago");
    draw(<MemoryPanel items={[photo(1, now - 3), photo(2, now - 5)]} />);
    expect(host.textContent).not.toContain("years ago");
  });

  it("draws nothing on a day with no photographs", () => {
    draw(<MemoryPanel items={[]} />);
    expect(host.querySelector(".panel")).toBeNull();
  });

  const film = (id: number, kind: string, year: number) => ({
    finished_at: new Date(year, 10, 8, 20).getTime() / 1000,
    item: { id, kind, title: `${kind} ${id}`, artwork: {} } as unknown as Item,
  });

  it("shows what you finished, with the right word, when there are no photographs", () => {
    const now = new Date().getFullYear();
    draw(<MemoryPanel items={[]} watched={[film(7, "movie", now - 1), film(8, "comic", now - 1)]} />);
    expect(host.querySelector(".panel")).not.toBeNull();
    expect([...host.querySelectorAll(".memory__note")].map((n) => n.textContent)).toEqual(["Watched", "Read"]);
    expect(host.textContent).toContain("A year ago");
    expect(finishedVerb("book")).toBe("Listened");
  });

  it("puts history first and keeps room for photographs", () => {
    const now = new Date().getFullYear();
    const watched = [1, 2, 3].map((id) => film(id, "movie", now - 1));
    const photos = [10, 11, 12, 13].map((id) => photo(id, now - 2));
    expect(memoryTiles(photos, watched).map((t) => t.item.id)).toEqual([1, 2, 10, 11]);
    // With no photographs, history may fill the panel.
    expect(memoryTiles([], [...watched, film(4, "movie", now - 1), film(5, "movie", now - 1)]).length).toBe(4);
    // With one photograph, it still gets its place.
    expect(memoryTiles([photo(10, now - 2)], watched).map((t) => t.item.id)).toEqual([1, 2, 10]);
  });
});
