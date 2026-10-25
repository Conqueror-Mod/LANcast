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
import { QueuePanel } from "./QueuePanel";

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

const rows = () => [...host.querySelectorAll("button.queue__row")];

async function render(ids: number[]) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <QueuePanel ids={ids} currentID={ids[0]} onPick={() => {}} inline />
      </QueryClientProvider>,
    );
  });
  await settle();
}

const shuffled = Array.from({ length: 1600 }, (_, i) => 250_000 + i);

describe("the queue panel on a whole-library shuffle", () => {
  it("asks for no row's item before any row is in view", async () => {
    await render(shuffled);
    expect(rows().length).toBe(1600); // every row is still there to scroll to
    expect(requested).toEqual([]);
  });

  it("asks only for the rows that come into view", async () => {
    await render(shuffled);
    await act(async () => observers[0].show(rows().slice(0, 20)));
    await settle();
    expect(requested.sort()).toEqual(shuffled.slice(0, 20));
    expect(host.textContent).toContain("Song 250000");
    // A row not yet seen keeps its number rather than a title.
    expect(host.textContent).toContain("Item 1600");
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

describe("the queue panel with nothing to measure with", () => {
  it("asks for every row, as it always did", async () => {
    vi.stubGlobal("IntersectionObserver", undefined);
    await render([1, 2, 3]);
    expect(requested.sort()).toEqual([1, 2, 3]);
  });
});
