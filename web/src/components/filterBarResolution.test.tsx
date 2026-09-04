/*
 * The Format filter, from the server's payload to the chip you press.
 *
 * This is the test that was missing when the resolution filter shipped broken,
 * and it is written to fail on that exact fault rather than around it.
 *
 * store.ResolutionBucket had no json tags, so the server sent Key/Label/
 * MinWidth/MaxWidth while docs/api.md promised key/label/min_width/max_width.
 * The client read the documented names, got undefined for both, and rendered
 * four chips with no text; pressing one sent `resolution=undefined`, which the
 * contract says is ignored rather than rejected. The filter did nothing, for as
 * long as it existed, and said nothing while doing it.
 *
 * Two suites had a chance. The Go test asserted b.MinWidth, which is the struct
 * and not the JSON. browseFilters.test.ts built its fixture in snake_case,
 * which is what the client believed rather than what it received. Neither
 * looked at a rendered chip, because FilterBar had no test at all.
 *
 * So: given the payload the contract actually promises, the chips must carry
 * the server's labels and must report the server's keys. jsdom performs no
 * layout, so this cannot prove the row is on screen or unobscured — but a chip
 * with no accessible name and a toggle carrying `undefined` are both visible to
 * it, and those were the failure.
 *
 * The Go side is internal/store/browsefilterwire_test.go, which asserts the
 * bytes. Between them the payload is checked at both ends of the wire, which is
 * the join neither suite was making.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { FilterBar } from "./FilterBar";
import type { Facets } from "@/api/types";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}

// Exactly what GET /api/libraries/{id}/facets is specified to send, including
// the boundaries sitting below the nominal widths — real 1080p is often 1912
// after cropping.
const facets: Facets = {
  initials: [],
  genres: [],
  decades: [],
  content_ratings: [],
  years: [],
  resolutions: [
    { key: "uhd", label: "4K", min_width: 3000, max_width: 0 },
    { key: "hd1080", label: "1080p", min_width: 1700, max_width: 2999 },
    { key: "hd720", label: "720p", min_width: 1100, max_width: 1699 },
    { key: "sd", label: "SD", min_width: 1, max_width: 1099 },
  ],
  collections: [],
  max_rating: 8.4,
  has_watched: false,
  has_in_progress: false,
  has_unmatched: false,
};

let container: HTMLDivElement;
let root: Root;
let onToggle: ReturnType<typeof vi.fn>;

async function render(params = new URLSearchParams()) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <MemoryRouter>
          <FocusProvider>
            <FilterBar
              libraryID={1}
              facets={facets}
              params={params}
              onToggle={onToggle}
              onSet={vi.fn()}
              onClear={vi.fn()}
            />
          </FocusProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
}

function buttonsByText(): string[] {
  return [...container.querySelectorAll("button")].map((b) =>
    (b.textContent ?? "").trim(),
  );
}

function findButton(text: string): HTMLButtonElement {
  const found = [...container.querySelectorAll("button")].find(
    (b) => (b.textContent ?? "").trim() === text,
  );
  if (!found) {
    throw new Error(
      `no button reading ${JSON.stringify(text)}; found ${JSON.stringify(buttonsByText())}`,
    );
  }
  return found as HTMLButtonElement;
}

async function click(el: HTMLElement) {
  await act(async () => {
    el.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  });
}

// The category button carries its active count, so it reads "Format" with
// nothing selected and "Format1" with one. Match the prefix rather than
// asserting a badge that is not what this file is about.
function findCategory(name: string): HTMLButtonElement {
  const found = [...container.querySelectorAll("button")].find((b) =>
    (b.textContent ?? "").trim().startsWith(name),
  );
  if (!found) {
    throw new Error(
      `no category button for ${JSON.stringify(name)}; found ${JSON.stringify(buttonsByText())}`,
    );
  }
  return found as HTMLButtonElement;
}

async function openFormat() {
  await render();
  await click(findCategory("Format"));
}

beforeEach(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response(JSON.stringify({ people: [] }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    ),
  );
  onToggle = vi.fn();
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  vi.unstubAllGlobals();
});

describe("the Format filter", () => {
  it("labels every chip from the server's bucket table", async () => {
    await openFormat();

    // The whole fault, stated positively: each chip carries the label the
    // server sent. When min_width/max_width arrived under other names these
    // were four empty strings.
    for (const label of ["4K", "1080p", "720p", "SD"]) {
      expect(
        buttonsByText(),
        `the Format panel should offer a chip reading ${label}`,
      ).toContain(label);
    }
  });

  it("renders no chip without an accessible name", async () => {
    await openFormat();

    // The failure was not a missing chip, it was four present ones with
    // nothing written on them — which every "does it render" assertion passes.
    const blank = [...container.querySelectorAll("button")].filter(
      (b) => (b.textContent ?? "").trim() === "",
    );
    expect(
      blank.length,
      "a chip with no text is unreadable and unpressable by name; " +
        "that is what an undefined label looks like on screen",
    ).toBe(0);
  });

  it("reports the server's key, never undefined", async () => {
    await openFormat();
    await click(findButton("4K"));

    expect(onToggle).toHaveBeenCalledWith("resolution", "uhd");

    // The specific regression: `resolution=undefined` is an unrecognised key,
    // and the contract says an unrecognised key is ignored rather than
    // rejected — so the grid came back unfiltered and nothing failed anywhere.
    for (const [, value] of onToggle.mock.calls) {
      expect(value).toBeDefined();
      expect(String(value)).not.toBe("undefined");
    }
  });

  it("shows a chip as pressed when its key is in the URL", async () => {
    await render(new URLSearchParams("resolution=hd1080"));
    await click(findCategory("Format"));

    expect(findButton("1080p").getAttribute("aria-pressed")).toBe("true");
    expect(findButton("4K").getAttribute("aria-pressed")).toBe("false");
  });
});
