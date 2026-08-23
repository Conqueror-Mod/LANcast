/*
 * The hand-queued lane, and the thing it must not break.
 *
 * "Play next" cannot be an insertion into `queue`: the shuffled order is
 * rebuilt whenever the queue's contents change, so adding one track would
 * reorder every other one as a side effect. The lane sits beside the queue
 * instead, and these assert both halves of that — the lane plays first, and the
 * queue resumes from where it left off rather than from wherever the queued
 * item happened to sit.
 *
 * That second half is the subtle one. `pos` is a cursor into `order`, and an
 * item played out of band is not at that cursor. Following it would leave the
 * cursor pointing at the wrong place, or — for an item that appears in the
 * queue elsewhere — at a plausible wrong place, which is worse.
 *
 * Driven through the real provider rather than a reimplementation of it,
 * because the bug this guards against would live in the wiring.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider, usePlayback } from "./PlaybackProvider";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
/** The live context, captured so a test can drive it directly. */
let pb: ReturnType<typeof usePlayback>;

function Probe() {
  pb = usePlayback();
  return (
    <div>
      <span data-testid="item">{pb.itemID}</span>
      <span data-testid="upnext">{pb.upNext.join(",")}</span>
      <span data-testid="hasnext">{String(pb.hasNext)}</span>
    </div>
  );
}

function read(id: string): string {
  return host.querySelector(`[data-testid="${id}"]`)?.textContent ?? "";
}

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response(JSON.stringify({ items: [], total: 0 }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    ),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <PlaybackProvider>
            <MemoryRouter>
              <Probe />
            </MemoryRouter>
          </PlaybackProvider>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await settle();
}

async function settle() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 5));
  });
}

/** Starts a queue of 1..n, playing the first. */
async function playQueue(ids: number[]) {
  await act(async () => {
    pb.play(ids[0], ids);
  });
  await settle();
}

describe("the hand-queued lane", () => {
  it("plays a queued item before the rest of the queue", async () => {
    await render();
    await playQueue([1, 2, 3]);
    expect(read("item")).toBe("1");

    await act(async () => {
      pb.playNextUp(99);
    });
    await settle();
    expect(read("upnext")).toBe("99");

    await act(async () => {
      pb.playNext();
    });
    await settle();
    // 99 rather than 2: that is the whole point of "play next".
    expect(read("item")).toBe("99");
    expect(read("upnext")).toBe("");
  });

  /*
   * The cursor test. After the lane drains, the queue must carry on from where
   * it was — not from wherever the queued item sat, and not from the start.
   */
  it("resumes the queue where it left off afterwards", async () => {
    await render();
    await playQueue([1, 2, 3]);

    await act(async () => {
      pb.playNextUp(99);
    });
    await settle();
    await act(async () => {
      pb.playNext();
    });
    await settle();
    expect(read("item")).toBe("99");

    await act(async () => {
      pb.playNext();
    });
    await settle();
    // 2, because the queue was left at 1. Following the queued item would have
    // lost the place entirely.
    expect(read("item")).toBe("2");
  });

  it("keeps insertion order between next and last", async () => {
    await render();
    await playQueue([1, 2]);

    await act(async () => {
      pb.addToQueue(50);
      pb.addToQueue(51);
      pb.playNextUp(49);
    });
    await settle();
    // "next" jumps the lane, "add" joins the back of it.
    expect(read("upnext")).toBe("49,50,51");
  });

  /*
   * A one-item queue has no next of its own — the case that stranded a show
   * resumed from Continue. Something queued by hand is a next regardless, and
   * the button must say so.
   */
  it("reports a next when only the lane has one", async () => {
    await render();
    await playQueue([1]);
    expect(read("hasnext")).toBe("false");

    await act(async () => {
      pb.addToQueue(7);
    });
    await settle();
    expect(read("hasnext")).toBe("true");
  });

  // Tracks lined up behind an album are about that album. Carrying them into a
  // film somebody has just started is a queue that surprises you.
  it("abandons the lane when the queue is replaced", async () => {
    await render();
    await playQueue([1, 2]);
    await act(async () => {
      pb.addToQueue(7);
    });
    await settle();
    expect(read("upnext")).toBe("7");

    await playQueue([40, 41]);
    expect(read("upnext")).toBe("");
  });

  // Queueing with nothing playing has no "next" to be after, and a control
  // that silently did nothing is one people press twice and then distrust.
  it("just plays when nothing is playing", async () => {
    await render();
    expect(read("item")).toBe("0");
    await act(async () => {
      pb.playNextUp(12);
    });
    await settle();
    expect(read("item")).toBe("12");
    expect(read("upnext")).toBe("");
  });
});
