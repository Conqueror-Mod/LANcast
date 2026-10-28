/*
 * A film opened before its previews exist (ADR 0105, queue-jumping): the
 * server answers "queued", and the player asks again until they are there.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { usePreviews } from "./hooks";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let answers: { status: number; body: unknown }[];
let asked: number;

const sheets = { interval_ms: 10000, columns: 10, rows: 10, width: 240, height: 136, sheets: 3 };

beforeEach(() => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  asked = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      const a = answers[Math.min(asked, answers.length - 1)];
      asked++;
      return new Response(JSON.stringify(a.body), { status: a.status, headers: { "Content-Type": "application/json" } });
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

function Probe() {
  const { data, queued } = usePreviews(7);
  return <p>{queued ? "queued" : data ? `sheets:${data.sheets}` : "none"}</p>;
}

async function mount() {
  await act(async () => {
    root.render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <Probe />
      </QueryClientProvider>,
    );
  });
  await act(async () => {
    await vi.advanceTimersByTimeAsync(10);
  });
}

describe("previews being made", () => {
  it("asks again while queued, and shows them once they are made", async () => {
    answers = [
      { status: 404, body: { error: { code: "queued", message: "being made next" } } },
      { status: 200, body: sheets },
    ];
    await mount();
    expect(host.textContent).toBe("queued");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(20_500);
    });
    expect(asked).toBe(2);
    expect(host.textContent).toBe("sheets:3");
  });

  it("does not ask again when there are simply none", async () => {
    answers = [{ status: 404, body: { error: { code: "not_found", message: "no previews" } } }];
    await mount();
    expect(host.textContent).toBe("none");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(60_000);
    });
    expect(asked).toBe(1);
  });
});
