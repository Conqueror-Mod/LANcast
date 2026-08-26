/*
 * Choosing which of its films a collection wears.
 *
 * The server's default — the earliest release — is right for almost every
 * franchise and wrong for some. This is the disagreement, and the assertions
 * that matter are about *when it offers what*: a reset that appears with
 * nothing to reset is as wrong as one that never appears.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { ChoosePoster } from "./ChoosePoster";
import type { Item } from "@/api/types";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let sent: { url: string; body: unknown }[];

function film(id: number, title: string, poster?: string): Item {
  return {
    id, title, kind: "movie", library_id: 1,
    artwork: poster ? { poster } : {},
  } as unknown as Item;
}

function collection(poster: string, inherited: boolean): Item {
  return {
    id: 99, title: "Marvel Cinematic Universe", kind: "collection",
    library_id: 1, artwork: { poster, inherited },
  } as unknown as Item;
}

async function render(col: Item, members: Item[]) {
  sent = [];
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    sent.push({ url: String(url), body: init?.body ? JSON.parse(String(init.body)) : null });
    return new Response(JSON.stringify({}), {
      status: 200, headers: { "Content-Type": "application/json" },
    });
  }));
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <MemoryRouter>
            <ChoosePoster collection={col} members={members} onClose={() => {}} />
          </MemoryRouter>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
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

const choices = () =>
  [...host.querySelectorAll<HTMLButtonElement>(".chooseposter__choice")];
const text = () => host.textContent ?? "";

describe("choosing a collection poster", () => {
  const members = [
    film(1, "Iron Man", "hash-ironman"),
    film(2, "Avengers: Endgame", "hash-endgame"),
  ];

  it("sends the chosen film, not the poster", async () => {
    await render(collection("hash-ironman", true), members);
    await act(async () => {
      choices()[1].click();
    });
    const put = sent.find((s) => s.url.includes("/poster"));
    expect(put?.body).toEqual({ from_item_id: 2 });
  });

  /*
   * `inherited` is the whole reason this can say anything useful. Without it
   * the dialog cannot tell "showing Iron Man because it is first" from
   * "showing Iron Man because somebody chose it".
   */
  it("says it is borrowing when nothing has been chosen", async () => {
    await render(collection("hash-ironman", true), members);
    expect(text()).toContain("borrowing");
    expect(text()).not.toContain("Use the default again");
  });

  it("offers a reset only once a choice has been made", async () => {
    await render(collection("hash-endgame", false), members);
    expect(text()).toContain("poster you chose");
    expect(text()).toContain("Use the default again");
  });

  // 0 clears the override on the server. Sending the current film instead
  // would look identical and would leave the lock in place for ever.
  it("resets by sending zero", async () => {
    await render(collection("hash-endgame", false), members);
    const reset = host.querySelector<HTMLButtonElement>(".chooseposter__reset")!;
    await act(async () => reset.click());
    const put = sent.find((s) => s.url.includes("/poster"));
    expect(put?.body).toEqual({ from_item_id: 0 });
  });

  // A member with no poster is a blank tile in a picker of pictures, and the
  // server refuses it anyway.
  it("offers only films that have a poster to lend", async () => {
    await render(collection("hash-ironman", true), [
      ...members,
      film(3, "Untagged", undefined),
    ]);
    expect(choices()).toHaveLength(2);
  });

  // Gold means where you are and nothing else (docs/design.md); here that is
  // the poster in use, which is exactly a selection.
  it("marks the poster currently in use", async () => {
    await render(collection("hash-endgame", false), members);
    const current = choices().filter((c) =>
      c.className.includes("chooseposter__choice--current"),
    );
    expect(current).toHaveLength(1);
    expect(current[0].textContent).toContain("Avengers: Endgame");
  });
});
