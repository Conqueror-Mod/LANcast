/*
 * The queue panel with the same track in it twice (R16-2).
 *
 * A playlist may hold a song at two positions (ADR 0030). The panel marked the
 * current row by id, so both copies lit up as playing, and keyed its rows by
 * id, so React saw two children with one key — the warning that comes before
 * a row is dropped or reused. In a long queue its window was centred on the
 * first copy, so the row actually playing could be outside what was drawn.
 */
import { describe, it, expect, beforeAll, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { QueuePanel, WINDOW_FROM } from "./QueuePanel";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let errors: string[];

beforeAll(() => {
  Element.prototype.scrollIntoView = () => {};
});

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  errors = [];
  vi.spyOn(console, "error").mockImplementation((...args: unknown[]) => {
    errors.push(args.map(String).join(" "));
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const id = Number(url.match(/\/api\/items\/(\d+)/)?.[1] ?? 0);
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
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

async function render(ids: number[], currentID: number, currentAt?: number) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        <QueuePanel inline ids={ids} currentID={currentID} currentAt={currentAt} onPick={() => {}} />
      </QueryClientProvider>,
    ),
  );
}

const rows = () => [...host.querySelectorAll<HTMLButtonElement>("button.queue__row")];
const current = () => rows().filter((r) => r.classList.contains("is-current"));

describe("a queue holding the same track twice", () => {
  it("marks only the copy that is playing", async () => {
    await render([5, 7, 8, 7], 7, 3);
    expect(rows()).toHaveLength(4);
    expect(current()).toHaveLength(1);
    expect(current()[0].querySelector(".queue__num")?.textContent).toBe("4");
  });

  it("keys its rows by position, so React sees no duplicate key", async () => {
    await render([5, 7, 8, 7], 7, 1);
    expect(errors.filter((e) => /same key/i.test(e))).toEqual([]);
  });

  it("marks the first copy when no position is given", async () => {
    await render([5, 7, 8, 7], 7);
    expect(current()).toHaveLength(1);
    expect(current()[0].querySelector(".queue__num")?.textContent).toBe("2");
  });

  it("marks nothing in the queue while a hand-queued track plays", async () => {
    await render([5, 7, 8, 7], 7, -1);
    expect(current()).toHaveLength(0);
  });

  // A long queue is windowed; the window has to be around the copy playing.
  it("draws the window around the copy that is playing", async () => {
    const n = WINDOW_FROM + 200;
    const ids = Array.from({ length: n }, (_, i) => 1000 + i);
    ids[5] = 42;
    ids[n - 5] = 42;
    await render(ids, 42, n - 5);
    expect(current()).toHaveLength(1);
    expect(current()[0].querySelector(".queue__num")?.textContent).toBe(String(n - 4));
  });
});
