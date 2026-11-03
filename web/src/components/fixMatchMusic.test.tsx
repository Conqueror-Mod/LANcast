/*
 * Fix match on an album and an artist (ADR 0115). An album is searched by
 * title and artist, so the dialog has an artist field, says what kind of
 * record each candidate is, and meters the artist instead of a popularity
 * MusicBrainz does not have. An artist is never searched by name: no search
 * box, and each candidate is a share of its albums, not a likeness.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { FocusProvider } from "@/focus/FocusController";
import { FixMatch } from "./FixMatch";
import type { Item } from "@/api/types";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let candidates: unknown[];
let urls: string[];

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  urls = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      urls.push(String(url));
      return new Response(JSON.stringify(candidates), {
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

const breakdown = { title: 1, year: 1, popularity: 0, total: 1, year_gap: 0 };

async function settle() {
  for (let i = 0; i < 4; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
  }
}

async function open(item: Partial<Item>) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <FixMatch item={{ id: 5, library_id: 1, title: "x", ...item } as Item} onClose={() => {}} />
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await settle();
}

function type(input: HTMLInputElement, value: string) {
  const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
  act(() => {
    set.call(input, value);
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
}

describe("Fix match on an album", () => {
  it("shows the credit, the record type and an artist meter, and no popularity", async () => {
    candidates = [
      {
        Provider: "musicbrainz", ExternalID: "rg-1", Kind: "album", Title: "1999",
        Artist: "Prince", ReleaseType: "Album", Year: 1982, Score: 1, PosterURL: "", Overview: "",
        Breakdown: { ...breakdown, artist: 1 },
      },
      {
        Provider: "musicbrainz", ExternalID: "rg-2", Kind: "album", Title: "1999",
        Artist: "Prince", ReleaseType: "Single", Year: 1982, Score: 1, PosterURL: "", Overview: "",
        Breakdown: { ...breakdown, artist: 1 },
      },
    ];
    await open({ kind: "album", title: "1999" });
    const [album, single] = [...host.querySelectorAll(".fixmatch__cand")];
    expect(album.querySelector(".fixmatch__kind")?.textContent).toBe("Album");
    expect(single.querySelector(".fixmatch__kind")?.textContent).toBe("Single");
    expect(album.querySelector(".fixmatch__artist")?.textContent).toBe("Prince");
    const meters = [...album.querySelectorAll(".fixmatch__meter-label")].map((m) => m.textContent);
    expect(meters).toEqual(["Title", "Artist", "Year"]);
  });

  it("searches with the artist somebody typed", async () => {
    candidates = [];
    await open({ kind: "album", title: "Panic! At The Disco" });
    const artist = host.querySelector<HTMLInputElement>('input[aria-label="Artist"]')!;
    expect(artist).not.toBeNull();
    type(artist, "Panic! at the Disco");
    await act(async () => {
      host.querySelector<HTMLButtonElement>(".fixmatch__go")!.click();
    });
    await settle();
    const last = new URL(urls[urls.length - 1], "http://x");
    expect(last.searchParams.get("artist")).toBe("Panic! at the Disco");
    expect(last.searchParams.get("q")).toBe("Panic! At The Disco");
  });

  it("links the current identity to its MusicBrainz page", async () => {
    candidates = [];
    await open({ kind: "album", title: "Hello Nasty", provider: "musicbrainz", external_id: "rg-9" });
    const a = host.querySelector<HTMLAnchorElement>(".fixmatch__current a")!;
    expect(a.href).toBe("https://musicbrainz.org/release-group/rg-9");
  });
});

describe("Fix match on an artist", () => {
  it("has no search box and states each candidate as a share of its albums", async () => {
    candidates = [
      {
        Provider: "musicbrainz", ExternalID: "a-1", Kind: "artist", Title: "Nirvana", Year: 0,
        Score: 0.75, PosterURL: "", Overview: "", Breakdown: { ...breakdown, total: 0.75 },
      },
      {
        Provider: "musicbrainz", ExternalID: "a-2", Kind: "artist", Title: "Nirvana", Year: 0,
        Score: 0, PosterURL: "", Overview: "", Breakdown: { ...breakdown, total: 0 },
      },
    ];
    await open({ kind: "artist", title: "Nirvana" });
    expect(host.querySelector(".fixmatch__input")).toBeNull();
    expect(host.querySelector(".fixmatch__why")?.textContent).toMatch(/identified by its albums/);
    const [top, none] = [...host.querySelectorAll(".fixmatch__cand")];
    expect(top.querySelector(".fixmatch__kind")?.textContent).toBe("Artist");
    expect(top.querySelector(".fixmatch__share")?.textContent).toBe("Credited on 75% of its matched albums");
    expect(top.querySelector(".fixmatch__meter")).toBeNull();
    expect(none.querySelector(".fixmatch__share")?.textContent).toBe("No matched album credits this artist");
  });
});
