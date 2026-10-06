/*
 * The duplicates page (ADR 0075): every copy with its album, removal through
 * the ordinary dialog with a sentence saying what it does to the albums, and a
 * list that drops the copy once it is removed.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { Duplicates, dimensions, nearRemovalNote, removalNote, splitGroups } from "./Duplicates";
import type { DuplicateGroup, NearCopyGroup } from "@/api/types";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const photo = (id: number, title: string) =>
  ({ id, kind: "photo", title, library_id: 5 }) as unknown as DuplicateGroup["copies"][number]["item"];

const crossAlbum: DuplicateGroup = {
  sha256: "9f2c",
  size_bytes: 2_271_689,
  copies: [
    { item: photo(1, "IMG_20231218"), album: "Animals" },
    { item: photo(2, "IMG_20231218"), album: "Me & Us" },
  ],
};

const sameFolder: DuplicateGroup = {
  sha256: "77aa",
  size_bytes: 900_000,
  copies: [
    { item: photo(3, "IMG_214134516"), album: "Resin Art" },
    { item: photo(4, "IMG_214134516(1)"), album: "Resin Art" },
    { item: photo(5, "IMG_214134516"), album: null },
  ],
};

describe("what removing a copy says", () => {
  it("names the album the photo leaves and where a copy stays", () => {
    expect(removalNote(crossAlbum, crossAlbum.copies[0])).toBe(
      'This removes the photo from "Animals". A copy stays in "Me & Us".',
    );
  });

  it("says another copy stays when the album keeps one", () => {
    expect(removalNote(sameFolder, sameFolder.copies[1])).toBe(
      'Another copy stays in "Resin Art".',
    );
  });

  it("calls a loose photo's place the library root", () => {
    expect(removalNote(sameFolder, sameFolder.copies[2])).toBe(
      'This removes the photo from the library root. A copy stays in "Resin Art".',
    );
  });
});

const allInOneAlbum: DuplicateGroup = {
  sha256: "aa11",
  size_bytes: 2_000_000,
  copies: [
    { item: photo(6, "Mackenzie 1"), album: "Mackenzie" },
    { item: photo(7, "Mackenzie 1"), album: "Mackenzie" },
  ],
};

const allAtTheRoot: DuplicateGroup = {
  sha256: "bb22",
  size_bytes: 1_000,
  copies: [
    { item: photo(8, "a"), album: null },
    { item: photo(9, "a"), album: null },
  ],
};

describe("splitting the groups", () => {
  // Every copy in one album is almost always an accident; the same photo in
  // two albums is often a decision. Shown apart, accidents first.
  it("puts groups whose copies share an album apart from those that do not", () => {
    const { sameAlbum, acrossAlbums } = splitGroups([crossAlbum, allInOneAlbum, sameFolder, allAtTheRoot]);
    expect(sameAlbum.map((g) => g.sha256)).toEqual(["aa11", "bb22"]);
    // sameFolder has two copies in Resin Art and one at the root: two places.
    expect(acrossAlbums.map((g) => g.sha256)).toEqual(["9f2c", "77aa"]);
  });
});

let host: HTMLDivElement;
let root: Root;
let admin = true;
let groups: DuplicateGroup[];
let near: NearCopyGroup[];
let pending: number;
let calls: { url: string; method: string }[];

beforeEach(() => {
  admin = true;
  groups = [crossAlbum];
  near = [];
  pending = 0;
  calls = [];
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const u = String(url);
      const method = init?.method ?? "GET";
      calls.push({ url: u, method });
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), { status: 200, headers: { "Content-Type": "application/json" } });
      if (method === "DELETE") {
        // The server removed it; the next read of the list no longer has it.
        groups = [];
        near = [];
        return new Response(null, { status: 204 });
      }
      if (u.includes("/near-copies")) {
        return json({ groups: near, extra_copies: near.reduce((n, g) => n + g.copies.length - 1, 0), pending });
      }
      if (u.includes("/duplicates")) {
        return json({ groups, extra_copies: groups.reduce((n, g) => n + g.copies.length - 1, 0) });
      }
      if (u.includes("/api/auth")) {
        return json({ user: { role: admin ? "admin" : "user" }, can_convert: true, configured: true, authenticated: true });
      }
      if (u.includes("/api/libraries")) return json([{ id: 5, name: "Photos", kind: "picture" }]);
      return json({});
    }),
  );
});

afterEach(() => {
  host.remove();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

async function flush() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 10));
  });
}

async function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <MemoryRouter initialEntries={["/library/5/duplicates"]}>
            <Routes>
              <Route path="/library/:id/duplicates" element={<Duplicates />} />
            </Routes>
          </MemoryRouter>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await flush();
  await flush();
}

const button = (label: string) =>
  [...host.querySelectorAll("button")].find((b) => b.textContent?.trim().startsWith(label));

describe("the duplicates page", () => {
  it("shows every copy with its album", async () => {
    await render();
    const albums = [...host.querySelectorAll(".dupes__album")].map((e) => e.textContent);
    expect(albums).toEqual(["Animals", "Me & Us"]);
    expect(host.textContent).toContain("1 extra copy");
  });

  it("offers removal to an admin, with what it does to the albums", async () => {
    await render();
    await act(async () => button("Remove…")!.click());
    await flush();
    expect(host.querySelector(".removedlg__note")?.textContent).toBe(
      'This removes the photo from "Animals". A copy stays in "Me & Us".',
    );
  });

  it("does not offer removal to anyone else", async () => {
    admin = false;
    await render();
    expect(button("Remove…")).toBeUndefined();
  });

  // The list is the reason the removal happened; it must not go on showing it.
  it("reads the list again after a copy is removed", async () => {
    await render();
    await act(async () => button("Remove…")!.click());
    await flush();
    await act(async () => button("Remove from library")!.click());
    await flush();
    await flush();
    expect(calls.some((c) => c.method === "DELETE" && c.url.includes("/api/items/1?mode=ignore"))).toBe(true);
    const reads = calls.filter((c) => c.method === "GET" && c.url.includes("/duplicates"));
    expect(reads.length).toBeGreaterThan(1);
    expect(host.querySelectorAll(".dupes__copy")).toHaveLength(0);
    expect(host.textContent).toContain("No duplicates");
  });

  it("shows the same-album groups first, under their own heading", async () => {
    groups = [crossAlbum, allInOneAlbum];
    await render();
    const heads = [...host.querySelectorAll(".dupes__section-head")].map((h) => h.firstChild?.textContent);
    expect(heads).toEqual(["In the same album", "Filed in more than one album"]);
    const albums = [...host.querySelectorAll(".dupes__album")].map((e) => e.textContent);
    expect(albums).toEqual(["Mackenzie", "Mackenzie", "Animals", "Me & Us"]);
  });

  it("says so when there are none", async () => {
    groups = [];
    await render();
    expect(host.textContent).toContain("No duplicates");
  });
});

/*
 * Near copies (ADR 0075, 2026-10-06 amendment): the same picture at another
 * size. Shown after the exact duplicates, with sizes, the largest marked, and
 * a removal sentence that names what stays.
 */
const sized = (id: number, w: number, h: number) =>
  ({ id, kind: "photo", title: `p${id}`, library_id: 5, width: w, height: h }) as unknown as DuplicateGroup["copies"][number]["item"];

const resized: NearCopyGroup = {
  keep: 10,
  copies: [
    { item: sized(10, 3648, 2736), album: "Party" },
    { item: sized(11, 720, 540), album: "Party" },
  ],
};

describe("near copies", () => {
  it("says what removing a smaller copy keeps", () => {
    expect(nearRemovalNote(resized, resized.copies[1])).toBe('The 3648 × 2736 copy stays in "Party".');
  });

  it("warns that removing the largest keeps the picture smaller", () => {
    expect(nearRemovalNote(resized, resized.copies[0])).toBe(
      "This is the largest copy. The 720 × 540 copy stays, so the picture is kept smaller.",
    );
  });

  it("reads a size as a person would", () => {
    expect(dimensions(sized(1, 4160, 3120))).toBe("4160 × 3120");
    expect(dimensions({ id: 2 } as never)).toBe("Size unknown");
  });

  it("shows each version's size and marks the largest, after the exact duplicates", async () => {
    near = [resized];
    await render();
    const heads = [...host.querySelectorAll(".dupes__section-head")].map((h) => h.firstChild?.textContent);
    expect(heads.at(-1)).toBe("Probably the same picture");
    const sizes = [...host.querySelectorAll(".dupes__size")].map((e) => e.textContent);
    expect(sizes).toEqual(["3648 × 2736Largest", "720 × 540"]);
  });

  it("is not called 'no duplicates' when only near copies exist", async () => {
    groups = [];
    near = [resized];
    await render();
    expect(host.textContent).not.toContain("No duplicates");
    expect(host.textContent).toContain("Probably the same picture");
  });

  it("says how many photos have not been compared yet", async () => {
    pending = 12;
    await render();
    expect(host.textContent).toContain("12 photos haven't been compared yet");
  });

  // The stale-list rule: removing a near copy must refresh this list too.
  it("reads the near-copy list again after a copy is removed", async () => {
    groups = [];
    near = [resized];
    await render();
    await act(async () => button("Remove…")!.click());
    await flush();
    expect(host.querySelector(".removedlg__note")?.textContent).toBe(
      "This is the largest copy. The 720 × 540 copy stays, so the picture is kept smaller.",
    );
    await act(async () => button("Remove from library")!.click());
    await flush();
    await flush();
    const reads = calls.filter((c) => c.method === "GET" && c.url.includes("/near-copies"));
    expect(reads.length).toBeGreaterThan(1);
    expect(host.querySelectorAll(".dupes__copy")).toHaveLength(0);
  });
});

