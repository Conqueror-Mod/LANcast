import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { MemoryRouter } from "react-router";
import { FocusProvider } from "@/focus/FocusController";
import { NewThisWeek } from "./NewThisWeek";
import { NEW_WINDOW_SECONDS, addedThisWeek, rowLabel } from "@/lib/newThisWeek";
import type { Item } from "@/api/types";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const NOW = 1_800_000_000;
const DAY = 24 * 60 * 60;

const item = (id: number, kind: string, daysAgo: number, over: Partial<Item> = {}): Item =>
  ({
    id,
    kind,
    title: `${kind} ${id}`,
    library_id: 1,
    artwork: {},
    added_at: NOW - daysAgo * DAY,
    missing: false,
    ...over,
  }) as unknown as Item;

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

describe("the seven-day window", () => {
  it("keeps the last seven days and nothing older", () => {
    const kept = addedThisWeek(
      [item(1, "movie", 0), item(2, "movie", 6.9), item(3, "movie", 7.1), item(4, "movie", 300)],
      NOW,
    );
    expect(kept.map((i) => i.id)).toEqual([1, 2]);
  });

  it("is a duration, so its edge is exactly seven days back", () => {
    const edge = item(1, "movie", 0, { added_at: NOW - NEW_WINDOW_SECONDS } as Partial<Item>);
    const past = item(2, "movie", 0, { added_at: NOW - NEW_WINDOW_SECONDS - 1 } as Partial<Item>);
    expect(addedThisWeek([edge, past], NOW).map((i) => i.id)).toEqual([1]);
  });

  it("leaves out a file the last scan could not find", () => {
    expect(addedThisWeek([item(1, "movie", 1, { missing: true })], NOW)).toEqual([]);
  });
});

describe("the video row's name", () => {
  it("says what is in it, once each, in the order it came", () => {
    expect(rowLabel([item(1, "show", 0), item(2, "movie", 0), item(3, "collection", 0)], "x")).toBe(
      "Shows · Films",
    );
    expect(rowLabel([item(1, "comic", 0)], "x")).toBe("Comics");
  });

  it("falls back when no kind is one it can name", () => {
    expect(rowLabel([item(1, "playlist", 0)], "Films and shows")).toBe("Films and shows");
  });
});

describe("New this week", () => {
  it("draws one heading over a row per kind that has something new", () => {
    draw(
      <NewThisWeek
        rows={{
          video: [item(1, "movie", 1)],
          music: [item(2, "artist", 2)],
          comics: [item(4, "comic", 3)],
          photos: [item(3, "photo", 30)],
        }}
        now={NOW}
      />,
    );
    expect(host.querySelectorAll("h2")).toHaveLength(1);
    expect(host.querySelector("h2")?.textContent).toBe("New this week");
    const labels = [...host.querySelectorAll(".shelf .section-label")].map((e) => e.textContent);
    expect(labels).toEqual(["Films", "Music", "Comics"]);
    // The photograph is a month old: no Photos row, rather than an old one.
    expect(host.textContent).not.toContain("Photos");
  });

  it("draws nothing in a week when nothing came in", () => {
    draw(
      <NewThisWeek
        rows={{ video: [item(1, "movie", 40)], music: [item(2, "artist", 9)], photos: undefined }}
        now={NOW}
      />,
    );
    expect(host.querySelector(".new-week")).toBeNull();
    expect(host.textContent).toBe("");
  });
});
