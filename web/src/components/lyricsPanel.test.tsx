/*
 * The lyrics panel.
 *
 * jsdom performs no layout and no scrolling, so nothing here proves the current
 * line is on screen. What it can prove is which line is current — which is the
 * part with a rule in it, and the part that is wrong in every naive
 * implementation: a forward scan lands on the wrong line after a seek, and
 * pre-highlighting the first line makes a long intro look broken.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { LyricsPanel } from "./LyricsPanel";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;

const synced = {
  source: "sidecar",
  synced: true,
  lines: [
    { at_ms: 10_000, text: "first line" },
    { at_ms: 20_000, text: "" },
    { at_ms: 30_000, text: "third line" },
  ],
};

function stub(body: unknown) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      new Response(JSON.stringify(body), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    ),
  );
}

async function render(atMS: number) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <LyricsPanel itemID={7} atMS={atMS} onClose={() => {}} />
      </QueryClientProvider>,
    );
  });
  await settle();
}

// React Query notifies on a macrotask, so a resolved promise is not enough.
async function settle() {
  for (let i = 0; i < 5; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  // jsdom has no scrollIntoView; the panel calls it on every line change.
  Element.prototype.scrollIntoView = vi.fn();
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

const current = () =>
  host.querySelector(".lyrics__line.is-current")?.textContent ?? null;
const text = () => host.textContent ?? "";

describe("following the song", () => {
  it("highlights the line whose moment has passed", async () => {
    stub(synced);
    await render(25_000);
    // 25s is past the second stamp and not yet the third.
    expect(current()).toBe("· · ·");
  });

  it("highlights nothing before the first line", async () => {
    /*
     * A real state, not an edge case: a song with a long intro should light
     * nothing rather than pre-highlighting its first words, which reads as the
     * panel being a line ahead of the music.
     */
    stub(synced);
    await render(2_000);
    expect(current()).toBeNull();
  });

  it("lands on the right line after a jump backwards", async () => {
    // A forward scan from wherever it was would stop at the first stamp it had
    // already passed and never move again.
    stub(synced);
    await render(31_000);
    expect(current()).toBe("third line");

    await act(async () => {
      root.render(
        <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
          <LyricsPanel itemID={7} atMS={11_000} onClose={() => {}} />
        </QueryClientProvider>,
      );
    });
    await settle();
    expect(current()).toBe("first line");
  });

  it("marks an instrumental break rather than leaving a gap", async () => {
    // A stamp with no words is how a lyric sheet says nothing is sung here;
    // rendering it as nothing would leave the previous line lit instead.
    stub(synced);
    await render(21_000);
    expect(current()).toBe("· · ·");
  });
});

describe("what it says when it cannot follow", () => {
  it("shows unsynced words and says they are not timed", async () => {
    // Words that do not scroll beat no words, and a panel that simply never
    // highlights anything reads as broken.
    stub({
      source: "sidecar",
      synced: false,
      lines: [{ at_ms: 0, text: "a line" }, { at_ms: 0, text: "another" }],
    });
    await render(30_000);
    expect(text()).toContain("not timed to the song");
    expect(text()).toContain("a line");
    expect(current()).toBeNull();
  });

  it("says where it looks when there are none", async () => {
    /*
     * The honest part. "No lyrics" invites "why not, go and find some" — and
     * this deliberately does not go looking online, so the panel says where it
     * did look and leaves the next move to a person.
     */
    stub({ source: "none", synced: false, lines: [] });
    await render(0);
    expect(text()).toContain("No lyrics for this track");
    expect(text()).toContain("does not go looking online");
  });
});
