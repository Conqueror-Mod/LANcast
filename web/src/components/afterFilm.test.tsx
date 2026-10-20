/*
 * The page a film ends on (AfterFilm).
 *
 * Asked for: when a film ends, suggest what to watch — the next film in its
 * collection when there is one, and something similar from outside the
 * franchise. What is asserted is that the two are told apart on screen and
 * that pressing one plays it. The ranking is the server's and tested there.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AfterFilm } from "./AfterFilm";
import type { Item } from "@/api/types";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const film = { id: 1, kind: "movie", title: "Saga 1", library_id: 1, artwork: {} } as Item;

let host: HTMLDivElement;
let root: Root;
let answer: unknown[] = [];
let asked: string[] = [];

beforeEach(() => {
  asked = [];
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      asked.push(url);
      return new Response(JSON.stringify({ items: answer, total: answer.length }), {
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

async function render(handlers: Partial<Parameters<typeof AfterFilm>[0]> = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <AfterFilm film={film} onPlay={() => {}} onAgain={() => {}} onClose={() => {}} {...handlers} />
      </QueryClientProvider>,
    );
  });
  for (let i = 0; i < 4; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

const button = (text: string) =>
  [...host.querySelectorAll("button")].find((b) => b.textContent?.includes(text));

describe("the end of a film", () => {
  it("offers the next film in its collection and a similar one, labelled apart", async () => {
    answer = [
      {
        id: 2,
        kind: "movie",
        title: "Saga 2",
        year: 2003,
        artwork: {},
        continues_from: { collection_id: 9, collection_title: "Saga Collection", after_id: 1, after_title: "Saga 1" },
      },
      { id: 7, kind: "movie", title: "Something Else", artwork: {} },
    ];
    const onPlay = vi.fn();
    await render({ onPlay });

    expect(asked.some((u) => u.endsWith("/api/items/1/afterwards"))).toBe(true);
    const cards = [...host.querySelectorAll(".after__card")].map((c) => c.textContent);
    expect(cards).toHaveLength(2);
    expect(cards[0]).toContain("Next in Saga Collection");
    expect(cards[0]).toContain("Saga 2");
    expect(cards[1]).toContain("More like Saga 1");
    expect(cards[1]).toContain("Something Else");

    await act(async () => {
      button("Saga 2")!.click();
    });
    expect(onPlay).toHaveBeenCalledWith(2);
  });

  it("says so when there is nothing to suggest, and still offers a way out", async () => {
    answer = [];
    const onAgain = vi.fn();
    const onClose = vi.fn();
    await render({ onAgain, onClose });
    expect(host.textContent).toContain("Nothing in this library is close enough to suggest.");
    await act(async () => {
      button("Watch again")!.click();
      button("Close")!.click();
    });
    expect(onAgain).toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
  });
});
