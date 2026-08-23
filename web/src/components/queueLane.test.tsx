/*
 * The queue panel shows what was queued by hand.
 *
 * Queueing something put it somewhere invisible. The lane is not in
 * `playOrder` and never will be — it sits beside the queue rather than being
 * inserted into it, so that adding one track does not reshuffle the other
 * 1,590 — and the panel only ever rendered `playOrder`. The single piece of
 * evidence that "play next" had worked was the track eventually playing.
 *
 * A control whose effect is invisible until later is one people press twice,
 * which for a queue means the thing plays twice.
 */
import { describe, it, expect, beforeAll, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { QueuePanel } from "./QueuePanel";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const titles: Record<number, string> = {
  1: "First",
  2: "Second",
  7: "Queued Song",
};

let host: HTMLDivElement;
let root: Root;
let dropped: number[];

// jsdom implements no scrolling at all, and the panel opens itself on the row
// that is playing. Not a stub for behaviour under test — just the one DOM call
// this environment does not have.
beforeAll(() => {
  Element.prototype.scrollIntoView = () => {};
});

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  dropped = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const m = url.match(/\/api\/items\/(\d+)/);
      const id = m ? Number(m[1]) : 0;
      return new Response(
        JSON.stringify({ id, title: titles[id] ?? `Item ${id}`, kind: "track" }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function render(upNext: number[]) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <MemoryRouter>
            <QueuePanel
              ids={[1, 2]}
              currentID={1}
              upNext={upNext}
              onPick={() => {}}
              onDrop={(at) => dropped.push(at)}
            />
          </MemoryRouter>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  for (let i = 0; i < 4; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

const text = () => host.textContent ?? "";

function dropButtons(): HTMLButtonElement[] {
  return [...host.querySelectorAll("button.queue__drop")] as HTMLButtonElement[];
}

describe("the queue panel's up-next lane", () => {
  it("says nothing about a lane that is empty", async () => {
    await render([]);
    expect(text()).not.toContain("Up next");
    expect(dropButtons().length).toBe(0);
  });

  // The bug: a queued track was in no list the panel drew.
  it("shows what was queued by hand", async () => {
    await render([7]);
    expect(text()).toContain("Up next");
    expect(text()).toContain("Queued Song");
    // And still shows the queue it is not part of.
    expect(text()).toContain("First");
  });

  /*
   * By position, not by id. The lane can hold the same track twice on purpose —
   * queueing a song again is a thing people do — and keying rows on id would
   * collapse the pair into one, silently shortening the list being read.
   */
  it("keeps both copies when the same track is queued twice", async () => {
    await render([7, 7]);
    expect(dropButtons().length).toBe(2);
  });

  it("drops the entry that was pressed, by position", async () => {
    await render([7, 1, 2]);
    act(() => dropButtons()[1].click());
    expect(dropped).toEqual([1]);
  });

  // Only the lane is removable. Taking a row out of the queue itself would
  // change its contents and rebuild the shuffled order, reordering everything
  // else as a side effect of dropping one row.
  it("offers no drop control on the queue itself", async () => {
    await render([7]);
    // One lane entry, one control — not one per queue row as well.
    expect(dropButtons().length).toBe(1);
  });
});
