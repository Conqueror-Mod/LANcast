/*
 * Tags and the heart (ADR 0062).
 *
 * The privacy is enforced by the server — the store takes an account on every
 * query and there is no shape of request that reaches somebody else's tags. So
 * these are about the three things this layer can get wrong:
 *
 *   the heart says which state it is in, to a screen reader as well as an eye
 *   adding a tag reaches the server rather than only the screen
 *   the screen says who can read this, because a person writing "needs a
 *   better copy" is entitled to know
 *
 * jsdom sees no layout, so nothing here is about how the heart looks. What it
 * can check is that the state is carried by something other than a colour,
 * which is the design constraint that made this component worth writing
 * carefully.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TagItem } from "./TagItem";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

type Tag = { id: number; name: string; count: number };

let host: HTMLDivElement;
let root: Root;
let sent: { url: string; method: string; body: string }[];
let state: { tags: Tag[]; favourite: boolean };

function mount(initial: { tags: Tag[]; favourite: boolean }) {
  state = initial;
  sent = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (method !== "GET") {
        sent.push({ url, method, body: String(init?.body ?? "") });
        if (url.includes("/favourite")) {
          state = { ...state, favourite: !state.favourite };
          return json({ favourite: state.favourite });
        }
        if (method === "POST") {
          const name = JSON.parse(String(init?.body)).name;
          const tag = { id: 99, name, count: 0 };
          state = { ...state, tags: [...state.tags, tag] };
          return json({ tag });
        }
        state = { ...state, tags: [] };
        return json({ removed: true });
      }
      if (url.includes("/tags")) return json(state);
      return json({});
    }),
  );
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

async function render() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <TagItem itemID={7} />
      </QueryClientProvider>,
    );
  });
  await settle();
}

async function settle() {
  for (let i = 0; i < 6; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

const heart = () =>
  host.querySelector("button[aria-pressed]") as HTMLButtonElement;

describe("tags and the heart", () => {
  /*
   * The favourite state is announced, not merely drawn.
   *
   * It cannot be carried by colour — design.md forbids gold for this and gold
   * is the only accent — so it is carried by a filled shape, which a screen
   * reader cannot see at all. `aria-pressed` is what makes the state exist for
   * somebody not looking at it.
   */
  it("says whether it is a favourite, in a way that does not depend on seeing it", async () => {
    mount({ tags: [], favourite: false });
    await render();
    expect(heart().getAttribute("aria-pressed")).toBe("false");
    expect(heart().getAttribute("aria-label")).toContain("Add to favourites");

    await act(async () => heart().click());
    await settle();

    expect(sent.some((s) => s.url.includes("/favourite"))).toBe(true);
    expect(heart().getAttribute("aria-pressed")).toBe("true");
    expect(heart().getAttribute("aria-label")).toContain("Remove");
  });

  // The heart's fill changes with the state, since that is what carries it for
  // everybody who is looking rather than listening.
  it("fills the heart when set and leaves it open when not", async () => {
    mount({ tags: [], favourite: true });
    await render();
    const path = host.querySelector("path")!;
    expect(path.getAttribute("fill")).toBe("currentColor");

    await act(async () => heart().click());
    await settle();
    expect(host.querySelector("path")!.getAttribute("fill")).toBe("none");
  });

  // Adding a tag reaches the server and clears the field, so the next one can
  // be typed without deleting the last.
  it("sends a new tag and empties the box", async () => {
    mount({ tags: [], favourite: false });
    await render();

    const input = host.querySelector("input") as HTMLInputElement;
    await act(async () => {
      const setter = Object.getOwnPropertyDescriptor(
        window.HTMLInputElement.prototype,
        "value",
      )!.set!;
      setter.call(input, "needs a better copy");
      input.dispatchEvent(new Event("input", { bubbles: true }));
    });
    await act(async () => {
      host
        .querySelector("form")!
        .dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    });
    await settle();

    const post = sent.find((s) => s.method === "POST");
    expect(post?.body).toContain("needs a better copy");
    expect((host.querySelector("input") as HTMLInputElement).value).toBe("");
    expect(host.textContent).toContain("needs a better copy");
  });

  // An empty tag is never sent. The server refuses it too; not sending it means
  // pressing return on an empty box does nothing rather than flashing an error.
  it("does not send an empty tag", async () => {
    mount({ tags: [], favourite: false });
    await render();

    await act(async () => {
      host
        .querySelector("form")!
        .dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    });
    await settle();
    expect(sent.filter((s) => s.method === "POST")).toHaveLength(0);
  });

  /*
   * The screen says who can read this.
   *
   * A person deciding whether to write something candid on an item is entitled
   * to know, and a feature people use carefully because they are unsure is one
   * that may as well not exist.
   */
  it("says the tags are private", async () => {
    mount({ tags: [], favourite: false });
    await render();
    expect(host.textContent).toContain("Only you can see");
  });
});
