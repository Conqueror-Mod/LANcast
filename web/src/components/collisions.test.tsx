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

/** The rename shape: the old path gone, the new one still here. */
const renamed = {
  ...pair,
  external_id: "324858",
  members: [
    { ...pair.members[0], id: 41, missing: true },
    { ...pair.members[1], id: 88, missing: false, edition: undefined },
  ],
};

// `use` is loosely typed so a test can add a field the fixture does not carry
// — dismissed_at arrives from the server and the literal above predates it.
function stub(
  compareAnswer?: Record<string, unknown>,
  use: Record<string, unknown> = pair,
) {
  asked = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      asked.push((init?.method ?? "GET") + " " + String(url));
      if ((init?.method ?? "GET") !== "GET") {
        return new Response(null, { status: 204 });
      }
      const compared = String(url).includes("compare=");
      const body = compared && compareAnswer
        ? { collisions: [{ ...use, ...compareAnswer }] }
        : { collisions: [use] };
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
    /*
     * Exhaustive, so a resolving control cannot arrive unnoticed under a name
     * the list above does not forbid.
     *
     * "I have looked at this" is not a resolution and is why the list has two
     * entries rather than one: nothing is merged, ranked or deleted, both files
     * stay exactly where they are, and the only thing recorded is that somebody
     * read the page. The report previously had no way to represent that, so a
     * film in two parts was listed again every time it opened.
     */
    expect(labels).toEqual(["i have looked at this", "compare bytes"]);
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

/*
 * Resolving the one collision that is not a judgement.
 *
 * ADR 0042's decision — never merge, rank or delete — is about a *server*
 * choosing between two files, and it holds. A renamed file is not that: the old
 * path is marked missing, the new one is added, and the pair is reported. On a
 * real library 34 of 43 collisions were exactly that, and the report could only
 * be read, never acted on.
 */
describe("forgetting a leftover row", () => {
  it("offers nothing on a collision of two files that are both here", async () => {
    stub();
    await render();
    expect(host.textContent).not.toContain("Forget this entry");
  });

  it("offers it on the row whose file has gone, and only that one", async () => {
    stub(undefined, renamed);
    await render();

    const offers = [...host.querySelectorAll("button")].filter(
      (b) => b.textContent?.trim() === "Forget this entry",
    );
    expect(offers.length).toBe(1);
  });

  /*
   * Confirmed in the page rather than through a native dialog. A frameless
   * Electron-style window cannot be relied on to give keyboard focus back after
   * one, which this project has already paid for once.
   */
  it("asks before forgetting, and says why it is safe", async () => {
    stub(undefined, renamed);
    await render();

    const offer = [...host.querySelectorAll("button")].find(
      (b) => b.textContent?.trim() === "Forget this entry",
    )!;
    await act(async () => {
      offer.click();
    });
    expect(host.textContent).toContain("The file is already gone");
    // Nothing has been sent yet: the first press opens the question.
    expect(asked.some((a) => a.startsWith("DELETE"))).toBe(false);
  });

  it("forgets through the mode that records nothing", async () => {
    stub(undefined, renamed);
    await render();

    const offer = [...host.querySelectorAll("button")].find(
      (b) => b.textContent?.trim() === "Forget this entry",
    )!;
    await act(async () => {
      offer.click();
    });
    const go = [...host.querySelectorAll("button")].find(
      (b) => b.textContent?.trim() === "Yes, forget it",
    )!;
    await act(async () => {
      go.click();
    });
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });

    /*
     * `forget`, not `ignore`. Ignoring records the path so a rescan never
     * re-adds it — but the file is gone, so there is nothing to suppress, and
     * nothing in the API or the client ever removes an ignored path.
     */
    const sent = asked.find((a) => a.startsWith("DELETE"));
    expect(sent).toContain("mode=forget");
    expect(sent).toContain("/api/items/41");
  });
});

/*
 * Answering the report.
 *
 * ADR 0042's decision is that a shared identity is reported and never resolved,
 * and the tests above guard that. This is the half it left out: a person who
 * has looked and decided the pair is fine had no way to say so, so a film in
 * two parts was listed again every time this page opened, for ever. A report
 * that cannot be answered is one people stop reading, and that cost falls on
 * the entries which do want attention.
 */
describe("looking at a collision", () => {
  it("sends the members rather than a handle", async () => {
    stub();
    await render();
    await act(async () => {
      host.querySelector<HTMLButtonElement>(".collide__seen")!.click();
    });
    await new Promise((r) => setTimeout(r, 5));

    const sent = asked.find((u) => u.includes("/collisions/dismiss"));
    expect(sent).toBeTruthy();
  });

  it("keeps a looked-at collision reachable rather than deleting it", async () => {
    // Shown behind a toggle that says how many there are. An action that
    // removes something with no trace cannot be checked or undone, which is the
    // failure this report already had in the other direction.
    stub(undefined, { ...pair, dismissed_at: 1_700_000_000 });
    await render();

    expect(host.textContent).toContain("you have looked at");
    // And not in the count of things still wanting attention.
    expect(host.querySelector(".review__count")?.textContent).toBe("0");
  });
});
