/*
 * Dialogs opened from a tile or the detail page, driven by a remote (R16-1).
 *
 * Arrows, OK and Back, and no Tab key. Before the fix the dialog's buttons were
 * registered with nothing and nothing kept the arrows inside it, so focus
 * stayed on the tile behind (PosterTile hands it back there when its menu
 * closes) and OK opened that tile. jsdom has no layout, so this cannot see
 * where anything is drawn; it proves where focus is and what OK reaches.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act, useState, type ReactNode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { FocusProvider, useFocusable } from "@/focus/FocusController";
import type { Item } from "@/api/types";
import { RemoveDialog } from "./RemoveDialog";
import { AddToPlaylist } from "./AddToPlaylist";
import { ChoosePoster } from "./ChoosePoster";
import { FixMatch } from "./FixMatch";
import { EditDescription } from "./EditDescription";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let opened: string[] = [];

const film = {
  id: 7,
  library_id: 1,
  kind: "movie",
  title: "Example Film",
  year: 2001,
  artwork: { poster: "p7" },
} as unknown as Item;
const member = { ...film, id: 8, title: "Another Film", artwork: { poster: "p8" } } as unknown as Item;

function Tile({ name }: { name: string }) {
  const f = useFocusable(() => void opened.push(name));
  return (
    <button {...f} data-tile={name}>
      {name}
    </button>
  );
}

// A grid behind, and a dialog opened over it from a tile.
function Screen({ dialog }: { dialog: (close: () => void) => ReactNode }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Tile name="tile A" />
      <Tile name="tile B" />
      <button data-open onClick={() => setOpen(true)}>
        open
      </button>
      {open && dialog(() => setOpen(false))}
    </>
  );
}

const dialogs: Array<[string, (close: () => void) => ReactNode]> = [
  ["Remove", (close) => <RemoveDialog item={film} onClose={close} onDone={close} />],
  ["Add to playlist", (close) => <AddToPlaylist item={film} onClose={close} />],
  [
    "Choose poster",
    (close) => <ChoosePoster collection={{ ...film, kind: "collection" } as Item} members={[film, member]} onClose={close} />,
  ],
  ["Fix match", (close) => <FixMatch item={film} onClose={close} />],
  ["Edit description", (close) => <EditDescription item={film} onClose={close} />],
];

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  opened = [];
  // Nothing these dialogs ask for needs answering to be focusable.
  vi.stubGlobal("fetch", vi.fn(() => new Promise<Response>(() => {})));
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function render(dialog: (close: () => void) => ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <FocusProvider>
          <MemoryRouter>
            <Screen dialog={dialog} />
          </MemoryRouter>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
}

const key = (k: string, shiftKey = false) =>
  act(async () => {
    (document.activeElement ?? document).dispatchEvent(
      new KeyboardEvent("keydown", { key: k, shiftKey, bubbles: true, cancelable: true }),
    );
  });

const tileA = () => host.querySelector<HTMLElement>('[data-tile="tile A"]')!;
const dialogEl = () => host.querySelector<HTMLElement>('[role="dialog"]');

// Opened the way a remote opens it: from a tile, with focus put back on that
// tile as PosterTile's menu does when it closes.
async function openFromTile() {
  await act(async () => {
    host.querySelector<HTMLButtonElement>("[data-open]")!.click();
    tileA().focus();
  });
}

describe("a dialog over a grid, with a remote", () => {
  for (const [name, dialog] of dialogs) {
    it(`${name}: focus moves into it, and OK never reaches the tile behind`, async () => {
      await render(dialog);
      await openFromTile();
      expect(dialogEl()).toBeTruthy();
      expect(dialogEl()!.contains(document.activeElement)).toBe(true);
      // Every control is one the arrows can reach: Save under a text box
      // included, which a remote had no way onto.
      const unreachable = [...dialogEl()!.querySelectorAll("button")].filter(
        (b) => !b.hasAttribute("data-focus-id"),
      );
      expect(unreachable.map((b) => b.textContent)).toEqual([]);

      await key("ArrowDown");
      await key("ArrowRight");
      await key("Enter");
      expect(dialogEl()?.contains(document.activeElement) ?? true).toBe(true);
      expect(opened).toEqual([]);
    });
  }

  it("lands on a choice, not on the ✕", async () => {
    await render(dialogs[0][1]);
    await openFromTile();
    expect(document.activeElement?.getAttribute("aria-label")).not.toBe("Close");
    expect(document.activeElement?.textContent).toContain("Remove from library");
  });

  it("keeps Tab inside, wrapping at both ends", async () => {
    await render(dialogs[0][1]);
    await openFromTile();
    const all = [...dialogEl()!.querySelectorAll<HTMLButtonElement>("button")];
    all[all.length - 1].focus();
    await key("Tab");
    expect(document.activeElement).toBe(all[0]);
    await key("Tab", true);
    expect(document.activeElement).toBe(all[all.length - 1]);
  });

  it("gives focus back to the tile it was opened from, on Back", async () => {
    await render(dialogs[0][1]);
    await openFromTile();
    expect(document.activeElement).not.toBe(tileA());
    await key("Escape");
    expect(dialogEl()).toBeNull();
    expect(document.activeElement).toBe(tileA());
  });
});
