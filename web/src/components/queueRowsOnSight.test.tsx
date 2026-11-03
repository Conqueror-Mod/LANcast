/*
 * The queue panel fetches a row's item only once the row is near view.
 *
 * "Shuffle the whole library" makes a queue of about 1,600 tracks, and every
 * row used to fetch its own item the moment Now Playing mounted the list.
 * Chromium fails that many simultaneous requests locally, and the first
 * track's own `/playback` question and direct stream were in the same burst:
 * an ordinary MP3 was reported as "would not play directly", the conversion was
 * refused because the file does play directly, and the first track of nearly
 * every whole-library shuffle did not play (item 255336, 2026-10-25).
 *
 * jsdom has no IntersectionObserver, so this stub stands in for one and lets
 * the test say which rows have come into view.
 */
import { describe, it, expect, beforeAll, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
// Counts row renders: every row calls useItem once per render.
const itemCalls = vi.hoisted(() => ({ n: 0 }));
vi.mock("@/api/hooks", async (importOriginal) => {
  const real = await importOriginal<typeof import("@/api/hooks")>();
  return {
    ...real,
    useItem: (...args: Parameters<typeof real.useItem>) => {
      itemCalls.n++;
      return real.useItem(...args);
    },
  };
});

import { QueuePanel, WINDOW_AFTER, WINDOW_BEFORE, WINDOW_FROM, WINDOW_STEP } from "./QueuePanel";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let requested: number[];
let observers: StubObserver[];

class StubObserver {
  observed = new Set<Element>();
  constructor(private readonly cb: IntersectionObserverCallback) {
    observers.push(this);
  }
  observe(el: Element) {
    this.observed.add(el);
  }
  unobserve(el: Element) {
    this.observed.delete(el);
  }
  disconnect() {
    this.observed.clear();
  }
  takeRecords() {
    return [];
  }
  /** Reports these elements as intersecting, the way a scroll would. */
  show(els: Element[]) {
    const entries = els
      .filter((el) => this.observed.has(el))
      .map((target) => ({ target, isIntersecting: true }) as IntersectionObserverEntry);
    this.cb(entries, this as unknown as IntersectionObserver);
  }
}

beforeAll(() => {
  Element.prototype.scrollIntoView = () => {};
});

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  requested = [];
  observers = [];
  vi.stubGlobal("IntersectionObserver", StubObserver);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const id = Number(url.match(/\/api\/items\/(\d+)/)?.[1] ?? 0);
      requested.push(id);
      return new Response(JSON.stringify({ id, title: `Song ${id}`, kind: "track" }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

const settle = async () => {
  for (let i = 0; i < 4; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
};

const rows = () => [...host.querySelectorAll<HTMLButtonElement>("button.queue__row")];

let picked: [number, number][] = [];

function view(ids: number[], current: number) {
  return (
    <QueryClientProvider client={client}>
      <QueuePanel ids={ids} currentID={current} onPick={(id, at) => picked.push([id, at])} inline />
    </QueryClientProvider>
  );
}

let client: QueryClient;

async function render(ids: number[], current = ids[0]) {
  picked = [];
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => root.render(view(ids, current)));
  await settle();
}

const more = () => host.querySelector<HTMLButtonElement>("button.queue__more:last-child");
const earlier = () => host.querySelector<HTMLButtonElement>("button.queue__more:first-child");
const numbers = () => rows().map((r) => Number(r.querySelector(".queue__num")?.textContent));

const shuffled = Array.from({ length: 1600 }, (_, i) => 250_000 + i);

describe("the queue panel on a whole-library shuffle", () => {
  it("asks for no row's item before any row is in view", async () => {
    await render(shuffled);
    expect(rows().length).toBe(WINDOW_AFTER); // a window, not 1,600 rows
    expect(requested).toEqual([]);
  });

  it("asks only for the rows that come into view", async () => {
    await render(shuffled);
    await act(async () => observers[0].show(rows().slice(0, 20)));
    await settle();
    expect(requested.sort()).toEqual(shuffled.slice(0, 20));
    expect(host.textContent).toContain("Song 250000");
    // A row not yet seen keeps its number rather than a title.
    expect(host.textContent).toContain("Item 150");
  });

  it("keeps a title once shown, after its row scrolls away", async () => {
    await render(shuffled);
    await act(async () => observers[0].show(rows().slice(0, 1)));
    await settle();
    // Seen once, then no longer watched: nothing can take the title back.
    expect(observers[0].observed.has(rows()[0])).toBe(false);
    expect(rows()[0].textContent).toContain("Song 250000");
  });
});

/*
 * The window (QueuePanel.tsx). Rendering all 9,000 rows of a shuffled library,
 * and re-rendering them on every tick of the playback clock, took the page's
 * main thread from under the visualizer.
 */
describe("the queue panel's window on a long queue", () => {
  it("draws the rows around the playing track, numbered as the queue numbers them", async () => {
    await render(shuffled, shuffled[800]);
    const n = numbers();
    expect(n[0]).toBe(801 - WINDOW_BEFORE);
    expect(n.at(-1)).toBe(800 + WINDOW_AFTER);
    expect(host.querySelector(".queue__row.is-current .queue__num")?.textContent).toBe("801");
    expect(earlier()?.textContent).toBe("770 earlier");
    expect(more()?.textContent).toBe(`${(1600 - 800 - WINDOW_AFTER).toLocaleString()} more`);
    // The count is still the whole queue's.
    expect(host.querySelector(".queue__count")?.textContent).toBe("1600");
  });

  it("grows when its end comes into view, and when the edge is pressed", async () => {
    await render(shuffled);
    await act(async () => observers[0].show([more()!]));
    await settle();
    expect(rows().length).toBe(WINDOW_AFTER + WINDOW_STEP);
    await act(async () => more()!.click());
    await settle();
    expect(rows().length).toBe(WINDOW_AFTER + 2 * WINDOW_STEP);
  });

  it("grows upward too", async () => {
    await render(shuffled, shuffled[800]);
    await act(async () => earlier()!.click());
    await settle();
    expect(numbers()[0]).toBe(801 - WINDOW_BEFORE - WINDOW_STEP);
  });

  it("follows the playing track out of the window", async () => {
    await render(shuffled);
    await act(async () => root.render(view(shuffled, shuffled[1200])));
    await settle();
    expect(host.querySelector(".queue__row.is-current .queue__num")?.textContent).toBe("1201");
  });

  it("picks by the queue's own position, not the row's place on the page", async () => {
    await render(shuffled, shuffled[800]);
    await act(async () => rows()[0].click());
    expect(picked).toEqual([[shuffled[800 - WINDOW_BEFORE], 800 - WINDOW_BEFORE]]);
  });

  it("draws a short queue whole, with no edges", async () => {
    await render(shuffled.slice(0, WINDOW_FROM), shuffled[200]);
    expect(rows().length).toBe(WINDOW_FROM);
    expect(more()).toBeNull();
  });

  /*
   * The tick of the playback clock re-renders the caller about four times a
   * second; it must not reach the rows. Counted by the one hook every row
   * calls, because the DOM cannot tell: React leaves it alone on a re-render
   * that changes nothing, so a DOM check passes on the old code too.
   */
  it("does not re-render its rows when only the caller does", async () => {
    await render(shuffled);
    itemCalls.n = 0;
    // A new onPick each time, as every caller passes.
    for (let i = 0; i < 5; i++) await act(async () => root.render(view(shuffled, shuffled[0])));
    expect(itemCalls.n).toBe(0);
  });

  it("re-renders only the two rows that change when the track moves on", async () => {
    await render(shuffled);
    itemCalls.n = 0;
    await act(async () => root.render(view(shuffled, shuffled[1])));
    expect(itemCalls.n).toBe(2);
  });
});

describe("the queue panel with nothing to measure with", () => {
  it("asks for every row, as it always did", async () => {
    vi.stubGlobal("IntersectionObserver", undefined);
    await render([1, 2, 3]);
    expect(requested.sort()).toEqual([1, 2, 3]);
  });
});
