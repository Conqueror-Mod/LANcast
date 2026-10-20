/*
 * What carries on after the thing being resumed (continueQueue).
 *
 * Reported: a collection's next film, resumed from the spotlight, played and
 * stopped — the franchise did not carry on the way a show does.
 */
import { describe, it, expect, afterEach, vi } from "vitest";
import { continueQueue } from "./continueQueue";

type Member = { id: number; watched?: boolean };

function serve(collections: Record<number, Member[]>, of: number[]) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), { status: 200, headers: { "Content-Type": "application/json" } });
      if (url.endsWith("/collections")) {
        return json({ items: of.map((id) => ({ id, kind: "collection", title: `C${id}` })) });
      }
      const m = url.match(/collection_id=(\d+)/);
      if (m) {
        return json({
          items: (collections[Number(m[1])] ?? []).map((x) => ({
            id: x.id,
            kind: "movie",
            title: `F${x.id}`,
            progress: x.watched ? { position_ms: 0, watched: true } : undefined,
          })),
        });
      }
      return new Response("nope", { status: 404 });
    }),
  );
}

afterEach(() => vi.unstubAllGlobals());

describe("continueQueue for a film", () => {
  it("carries on through the rest of its collection", async () => {
    serve({ 900: [{ id: 1 }, { id: 2 }, { id: 3 }, { id: 4 }] }, [900]);
    expect(await continueQueue({ id: 2, kind: "movie" })).toEqual([2, 3, 4]);
  });

  // The server's rule: a film already watched is not the next one.
  it("skips films after it that are already watched", async () => {
    serve({ 900: [{ id: 1 }, { id: 2 }, { id: 3, watched: true }, { id: 4 }] }, [900]);
    expect(await continueQueue({ id: 2, kind: "movie" })).toEqual([2, 4]);
  });

  it("has no queue for the last film, or one in no collection", async () => {
    serve({ 900: [{ id: 1 }, { id: 2 }] }, [900]);
    expect(await continueQueue({ id: 2, kind: "movie" })).toBeUndefined();
    serve({}, []);
    expect(await continueQueue({ id: 2, kind: "movie" })).toBeUndefined();
  });

  // A franchise and a box set: the one the caller names is the one continued.
  it("continues the collection it was asked about", async () => {
    serve({ 900: [{ id: 2 }, { id: 3 }], 901: [{ id: 2 }, { id: 7 }] }, [900, 901]);
    expect(await continueQueue({ id: 2, kind: "movie" }, 901)).toEqual([2, 7]);
    expect(
      await continueQueue({
        id: 2,
        kind: "movie",
        continues_from: { collection_id: 901, collection_title: "B", after_id: 1, after_title: "A" },
      }),
    ).toEqual([2, 7]);
  });

  it("answers no queue when the lookup fails", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("nope", { status: 500 })));
    expect(await continueQueue({ id: 2, kind: "movie" })).toBeUndefined();
  });
});
