/*
 * The browser player's game save is only ever sent over the save it started
 * from (retroSaves.ts, R12-4).
 *
 * The server is a fake that keeps one slot and honours based_on as putSave
 * does: 409, and nothing changed, when the slot holds anything else.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { fetchSave, listSaves, NO_SAVE, SRAMSync } from "./retroSaves";

const bytes = (s: string) => new TextEncoder().encode(s);

async function sha(b: Uint8Array): Promise<string> {
  const d = await crypto.subtle.digest("SHA-256", b as BufferSource);
  return [...new Uint8Array(d)].map((x) => x.toString(16).padStart(2, "0")).join("");
}

// jsdom's Blob, which Node's Response cannot read, read the way jsdom can.
function readBlob(b: Blob): Promise<Uint8Array> {
  return new Promise((resolve, reject) => {
    const r = new FileReader();
    r.onload = () => resolve(new Uint8Array(r.result as ArrayBuffer));
    r.onerror = () => reject(r.error);
    r.readAsArrayBuffer(b);
  });
}

interface Fake {
  slot: Uint8Array | null;
  puts: string[]; // based_on of each PUT
  down: boolean; // every request fails, as a restarting server's do
}

let fake: Fake;

beforeEach(() => {
  fake = { slot: null, puts: [], down: false };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string, init?: RequestInit) => {
      if (fake.down) return new Response("restarting", { status: 503 });
      const url = new URL(input, "http://lancast.test");
      if (url.pathname === "/api/items/7/saves") {
        return Response.json({ saves: fake.slot ? [{ slot: "sram" }] : [] });
      }
      if (init?.method === "PUT") {
        const base = url.searchParams.get("based_on") ?? "";
        fake.puts.push(base);
        const now = fake.slot ? await sha(fake.slot) : NO_SAVE;
        if (base && base !== now) return new Response("{}", { status: 409 });
        fake.slot = await readBlob(init.body as Blob);
        return Response.json({ slot: "sram", sha256: await sha(fake.slot) });
      }
      if (!fake.slot) return new Response("{}", { status: 404 });
      return new Response(fake.slot as BodyInit, { headers: { ETag: `"${await sha(fake.slot)}"` } });
    }),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("the browser's game save", () => {
  it("tells a failed fetch apart from an empty slot", async () => {
    expect(await fetchSave(7, "sram")).toBeNull();
    expect(await listSaves(7)).toEqual([]);
    fake.down = true;
    await expect(fetchSave(7, "sram")).rejects.toThrow();
    expect(await listSaves(7)).toBeNull();
  });

  it("sends each save based on the one before it", async () => {
    fake.slot = bytes("real save");
    const loaded = await fetchSave(7, "sram");
    expect(loaded?.sha).toBe(await sha(bytes("real save")));
    const sync = new SRAMSync(7, loaded);

    expect(await sync.store(bytes("real save"))).toBe("unchanged");
    expect(await sync.store(bytes("played on"))).toBe("sent");
    expect(await sync.store(bytes("and on"))).toBe("sent");
    expect(new TextDecoder().decode(fake.slot!)).toBe("and on");
    expect(fake.puts).toEqual([await sha(bytes("real save")), await sha(bytes("played on"))]);
  });

  it("does not send over a save another player made, and then sends nothing", async () => {
    fake.slot = bytes("real save");
    const sync = new SRAMSync(7, await fetchSave(7, "sram"));
    fake.slot = bytes("saved on the laptop");

    expect(await sync.store(bytes("stale"))).toBe("conflict");
    expect(sync.stopped).toBe(true);
    expect(await sync.store(bytes("staler"))).toBe("held");
    expect(fake.puts).toHaveLength(1);
    expect(new TextDecoder().decode(fake.slot!)).toBe("saved on the laptop");
  });

  it("does not send a first save over one that appeared after an empty load", async () => {
    const sync = new SRAMSync(7, await fetchSave(7, "sram"));
    fake.slot = bytes("saved elsewhere");
    expect(await sync.store(bytes("blank"))).toBe("conflict");
    expect(fake.puts).toEqual([NO_SAVE]);
    expect(new TextDecoder().decode(fake.slot!)).toBe("saved elsewhere");
  });

  it("sends overlapping stores one after the other", async () => {
    const sync = new SRAMSync(7, null);
    const [a, b] = await Promise.all([sync.store(bytes("one")), sync.store(bytes("two"))]);
    expect([a, b]).toEqual(["sent", "sent"]);
    expect(new TextDecoder().decode(fake.slot!)).toBe("two");
  });
});
