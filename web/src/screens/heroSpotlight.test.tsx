/*
 * The homepage spotlight, wired up.
 *
 * heroMode.test.ts covers the choosing. What jsdom can prove on top of that is
 * the wiring: that a mode nobody chose costs no requests, that a suggestion
 * asks for the right thing, and that pinning from an item actually changes what
 * the home page will show — a pin that stored an id and altered nothing you can
 * see is the quiet-failure shape this project keeps rediscovering.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import {
  HERO_MODE_KEY,
  HERO_PINNED_KEY,
  HERO_MODE_DEFAULT,
  useHeroMode,
  usePinnedHero,
} from "@/lib/heroMode";
// Written through the store rather than into localStorage: useDevice keeps a
// process-lifetime cache, so a key it has already read once does not notice a
// value planted underneath it. That is the right behaviour for the app and a
// trap for a test suite sharing one module instance.
import { writeDevice } from "@/lib/device";
import { useHeroSpotlight } from "@/lib/useHero";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let asked: string[];

const film = {
  id: 1, library_id: 3, kind: "movie", title: "Half-watched", missing: false,
  artwork: { fanart: "/f.jpg" }, genres: ["Horror", "Thriller"],
};
const suggestion = {
  id: 2, library_id: 3, kind: "movie", title: "Never watched", missing: false,
  artwork: { fanart: "/g.jpg" },
};

/*
 * A track at the top of the list, which is where a real one usually is.
 *
 * Continue Watching is one mixed list ordered by when you last played
 * something; Home splits it for display and the underlying list does not. So
 * the top row belongs to whoever listened to music most recently — and a track
 * has no genres and lives in a library the spotlight excludes entirely.
 */
const track = {
  id: 9, library_id: 18, kind: "track", title: "Building Better Worlds",
  missing: false, artwork: {},
};

function stub() {
  asked = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const u = String(url);
      asked.push(u);
      let body: unknown = {};
      if (u.includes("/api/continue")) body = { items: [track, film] };
      else if (u.startsWith("/api/items?")) body = { items: [suggestion], total: 1 };
      else if (u.startsWith("/api/items/")) {
        /*
         * Answer with the item that was asked for, not with the film every
         * time. A stub that hands back a usable record whatever the id cannot
         * tell "seeded from the track" apart from "seeded from the film" — and
         * that is precisely the bug under test.
         */
        body = u.endsWith("/9") ? track : film;
      }
      return new Response(JSON.stringify(body), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }),
  );
}

// The hook has no markup of its own, so a probe renders what it chose.
function Probe() {
  const pick = useHeroSpotlight([]);
  if (!pick) return <span>nothing</span>;
  return (
    <span>
      {pick.reason}:{pick.item.title}
      {pick.seed ? ` from ${pick.seed.title}` : ""}
    </span>
  );
}

async function render(ui: React.ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <MemoryRouter>{ui}</MemoryRouter>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await settle();
}

// React Query notifies on a macrotask, so a resolved promise is not enough.
async function settle() {
  for (let i = 0; i < 6; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

beforeEach(() => {
  localStorage.clear();
  writeDevice(HERO_MODE_KEY, HERO_MODE_DEFAULT);
  writeDevice(HERO_PINNED_KEY, 0);
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  localStorage.clear();
});

const text = () => host.textContent ?? "";

describe("the spotlight pays only for the mode it is in", () => {
  it("asks for nothing extra by default", async () => {
    /*
     * Home already fetches Continue Watching and Recently Added for its own
     * shelves, so the two default modes must cost no request at all. This is
     * the first screen of the app; a spotlight that added two round trips for
     * a feature nobody switched on would be a tax on everybody.
     */
    stub();
    await render(<Probe />);
    expect(asked.some((u) => u.startsWith("/api/items?"))).toBe(false);
    expect(text()).toContain("resuming:Half-watched");
  });

  it("looks the seed up in full when it is suggesting", async () => {
    // Genres are a detail response only, so the list shape cannot answer this.
    stub();
    writeDevice(HERO_MODE_KEY, "recommended");
    await render(<Probe />);
    expect(asked).toContain("/api/items/1");
  });

  it("asks for something unwatched sharing a genre", async () => {
    stub();
    writeDevice(HERO_MODE_KEY, "recommended");
    await render(<Probe />);

    const query = asked.find((u) => u.startsWith("/api/items?"));
    expect(query, "no candidate search was made").toBeTruthy();
    expect(query).toContain("watched=false");
    expect(query).toContain("genre=Horror");
    // The seed's own library. A suggestion from a different library is not
    // "more of this", it is just something else.
    expect(query).toContain("library_id=3");
  });

  it("says which film a suggestion came from", async () => {
    // The claim has to be attributable, or it is an engine.
    stub();
    writeDevice(HERO_MODE_KEY, "recommended");
    await render(<Probe />);
    expect(text()).toContain("recommended:Never watched from Half-watched");
  });
});

describe("pinning", () => {
  // A small stand-in for the detail page's button, wired the same way, so this
  // asserts the contract rather than the layout around it.
  function PinButton({ id }: { id: number }) {
    const [pinnedID, setPinned] = usePinnedHero();
    const [mode, setMode] = useHeroMode();
    return (
      <button
        onClick={() => {
          setPinned(id);
          setMode("pinned");
        }}
      >
        {`pin (${mode}/${pinnedID})`}
      </button>
    );
  }

  it("changes what the spotlight will show, not just what is stored", async () => {
    /*
     * The failure worth guarding: the id is written, the store is right, and
     * the home page carries on showing the last thing anybody watched.
     */
    stub();
    await render(<PinButton id={42} />);
    await act(async () => {
      host.querySelector("button")!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    await settle();

    expect(text()).toContain("pinned/42");
    expect(JSON.parse(localStorage.getItem(HERO_MODE_KEY)!)).toBe("pinned");
    expect(JSON.parse(localStorage.getItem(HERO_PINNED_KEY)!)).toBe(42);
  });
});

describe("what the suggestion is seeded from", () => {
  /*
   * The fault that shipped in v0.9.22 and survived three readings of the code.
   *
   * Seeding from the literal first row meant a track, which has no genres, so
   * the candidate query switched itself off and Suggested silently became
   * Continue watching. Found by asking the running app what the seed was.
   */
  it("skips a track at the top of the list", async () => {
    stub();
    writeDevice(HERO_MODE_KEY, "recommended");
    await render(<Probe />);

    // The film's detail, not the track's — 1 is the film, 9 is the track.
    expect(asked).toContain("/api/items/1");
    expect(asked).not.toContain("/api/items/9");
  });

  it("still asks for candidates when a track leads the list", async () => {
    // The symptom: with the track as seed there was no query at all.
    stub();
    writeDevice(HERO_MODE_KEY, "recommended");
    await render(<Probe />);

    const query = asked.find((u) => u.startsWith("/api/items?"));
    expect(query, "no candidate search was made").toBeTruthy();
    expect(query).toContain("genre=Horror");
    expect(query).toContain("library_id=3");
  });
});
