/*
 * A saved star rating is not gold at rest (R16-4).
 *
 * `.rate__star.is-full` was full gold, and is-full follows the saved score
 * whenever nothing is hovered, so four gold stars sat on every film you had
 * rated while focus was somewhere else. Gold means where you are. The saved
 * score is drawn in the text colour now, and the stars light gold only under
 * the pointer (the row's is-previewing) or keyboard focus. The colours are
 * CSS, which jsdom does not compute; what is asserted is the class the CSS
 * keys on.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RateItem } from "./RateItem";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      new Response(JSON.stringify({ rating: { score: 8, review: "" } }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    ),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <RateItem itemID={700} />
      </QueryClientProvider>,
    );
  });
  await act(async () => {
    await new Promise((r) => setTimeout(r, 5));
  });
}

const row = () => host.querySelector(".rate__stars")!;
const full = () => host.querySelectorAll(".rate__star.is-full").length;

describe("your own rating", () => {
  it("shows the saved score without lighting it as where you are", async () => {
    await render();
    expect(full()).toBe(4);
    expect(row().classList.contains("is-previewing")).toBe(false);
  });

  it("lights the stars while the pointer is on them, and stops when it leaves", async () => {
    await render();
    const second = host.querySelectorAll<HTMLButtonElement>(".rate__star")[1];
    await act(async () => {
      second.dispatchEvent(new MouseEvent("mouseover", { bubbles: true }));
    });
    expect(row().classList.contains("is-previewing")).toBe(true);
    expect(full()).toBe(2);
    await act(async () => {
      row().dispatchEvent(new MouseEvent("mouseout", { bubbles: true, relatedTarget: document.body }));
    });
    expect(row().classList.contains("is-previewing")).toBe(false);
    expect(full()).toBe(4);
  });
});
