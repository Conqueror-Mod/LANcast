/*
 * The collision report (ADR 0042).
 *
 * The load-bearing assertion in this file is a *negative* one: there is no
 * merge, no "keep this one", no delete. That is the decision, not a gap, and it
 * is the kind of thing a later feature adds back without noticing what it is
 * overturning — so it is asserted rather than assumed.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { Collisions } from "./Collisions";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let asked: string[];

const pair = {
  provider: "tmdb",
  external_id: "324857",
  same_size: true,
  members: [
    {
      id: 41, title: "Spider-Verse", path: "W:/Films/Spider-Verse (2018).mkv",
      size_bytes: 2832374353, library_id: 1, missing: false,
    },
    {
      id: 88, title: "Spider-Verse",
      path: "W:/Films/Spider-Verse (Alternate Cut) (2018).mkv",
      edition: "Alternate Cut",
      size_bytes: 2832374353, library_id: 1, missing: false,
    },
  ],
};

function stub(compareAnswer?: Record<string, unknown>) {
  asked = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      asked.push(String(url));
      const compared = String(url).includes("compare=");
      const body = compared && compareAnswer
        ? { collisions: [{ ...pair, ...compareAnswer }] }
        : { collisions: [pair] };
      return new Response(JSON.stringify(body), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }),
  );
}

async function render(enabled = true) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <MemoryRouter>
            <Collisions enabled={enabled} />
          </MemoryRouter>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await settle();
}

// The query has to resolve before anything is on screen; the component renders
// nothing while it is loading. Same shape the other screen suites use.
async function settle() {
  for (let i = 0; i < 4; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
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

const text = () => host.textContent ?? "";

describe("two files, one work", () => {
  it("shows both paths, because that is the whole report", async () => {
    stub();
    await render();
    expect(text()).toContain("Spider-Verse (2018).mkv");
    expect(text()).toContain("Spider-Verse (Alternate Cut) (2018).mkv");
  });

  // The marker is displayed and not believed. This pair was byte-identical.
  it("shows what the filename claimed", async () => {
    stub();
    await render();
    expect(text()).toContain("Alternate Cut");
  });

  /*
   * The decision, asserted. LANcast reports the collision and resolves none of
   * it: a shared provider id can mean a redundant copy, a second edition, one
   * film in two parts, or a file that is wrong about itself — and two of the
   * thirteen pairs that motivated this were not duplicates at all.
   */
  it("offers no way to merge, rank or delete", async () => {
    stub();
    await render();
    const labels = [...host.querySelectorAll("button")].map((b) =>
      (b.textContent ?? "").toLowerCase(),
    );
    for (const forbidden of ["merge", "delete", "remove", "keep", "hide", "ignore"]) {
      expect(labels.join(" "), `offers to ${forbidden}`).not.toContain(forbidden);
    }
    // The only control reads bytes.
    expect(labels).toEqual(["compare bytes"]);
  });

  /*
   * Comparison is opt-in: sampling three windows of a 14.6 GB file is expensive
   * next to nothing, and a report is opened far more often than a row in it is
   * investigated.
   */
  it("does not read any bytes until asked", async () => {
    stub();
    await render();
    expect(asked.some((u) => u.includes("compare="))).toBe(false);

    await act(async () => {
      host.querySelector<HTMLButtonElement>(".collide__compare")!.click();
    });
    await settle();
    expect(asked.some((u) => u.includes("compare=324857"))).toBe(true);
  });

  /*
   * The wording is the claim. Three 1 MB windows and a size cannot prove two
   * files equal, so the report must not say "identical" flat — it is a
   * defensible shortcut only because nothing acts on the answer.
   */
  it("never claims more than it sampled", async () => {
    stub({ same_bytes: true });
    await render();
    await act(async () => {
      host.querySelector<HTMLButtonElement>(".collide__compare")!.click();
    });
    await settle();
    expect(text()).toContain("so far as sampled");
  });

  // A file that could not be read is an absence of evidence, not a difference.
  it("does not report an unreadable file as a different file", async () => {
    stub({
      members: [pair.members[0], { ...pair.members[1], unreadable: true }],
    });
    await render();
    await act(async () => {
      host.querySelector<HTMLButtonElement>(".collide__compare")!.click();
    });
    await settle();
    expect(text()).toContain("Could not be read");
    expect(text()).not.toContain("Contents differ");
  });

  // Admin-only on the server, so a member must not fire a request that 403s.
  it("asks for nothing when the viewer is not an admin", async () => {
    stub();
    await render(false);
    expect(asked).toEqual([]);
    expect(text()).toBe("");
  });
});
