import { apiGet } from "@/api/client";

/*
 * The browser player's saves on the server (romsaves.go).
 *
 * Kept apart from the screen so the one rule that matters can be tested
 * without booting a core: the game's own save is only ever sent over the save
 * it started from (R12-4). A browser that could not fetch the save would
 * otherwise play on blank save RAM, which many cartridges write at boot, and
 * its first upload would replace the real save on the server.
 */

/** One stored save, as the save list gives it. */
export interface StoredSave {
  slot: string;
  core?: string;
  updated_at?: number;
}

/** A save as fetched: its bytes, and the SHA-256 the server knows it by. */
export interface FetchedSave {
  bytes: Uint8Array;
  sha: string;
}

/** What a writer that loaded an empty slot names as its base. */
export const NO_SAVE = "none";

const SHA256 = /^[0-9a-f]{64}$/;

/** This person's saves for one game, or null when the server could not say. */
export async function listSaves(itemID: number): Promise<StoredSave[] | null> {
  try {
    const r = await apiGet<{ saves: StoredSave[] }>(`/api/items/${itemID}/saves`);
    return r?.saves ?? [];
  } catch {
    return null;
  }
}

/**
 * One save, or null when the slot is empty. Throws when it could not be
 * fetched, which is not the same thing: an empty slot is safe to save into,
 * and a save that did not arrive is not.
 */
export async function fetchSave(itemID: number, slot: string): Promise<FetchedSave | null> {
  const res = await fetch(`/api/items/${itemID}/saves/${slot}`, { credentials: "same-origin" });
  if (res.status === 404) return null;
  if (!res.ok) throw new Error(`the ${slot} save could not be fetched (${res.status})`);
  // The ETag is the save's SHA-256, quoted.
  const sha = (res.headers.get("ETag") ?? "").replace(/^W\//, "").replace(/"/g, "");
  if (!SHA256.test(sha)) throw new Error(`the ${slot} save came without its checksum`);
  return { bytes: new Uint8Array(await res.arrayBuffer()), sha };
}

export type PutResult = { ok: true; sha: string } | { ok: false; conflict: boolean };

/*
 * Stores one save. A state names the core and version that wrote it, as the
 * server requires; the game's own save names neither, because every player
 * reads it. basedOn makes the write conditional: the server answers 409, and
 * changes nothing, when the slot no longer holds that save.
 */
export async function putSave(
  itemID: number,
  slot: string,
  data: Uint8Array,
  opts: { core?: string; version?: string; basedOn?: string } = {},
): Promise<PutResult> {
  const q = new URLSearchParams();
  if (opts.core) {
    q.set("core", opts.core);
    q.set("core_version", opts.version || "unknown");
  }
  if (opts.basedOn) q.set("based_on", opts.basedOn);
  const qs = q.toString();
  try {
    const res = await fetch(`/api/items/${itemID}/saves/${slot}${qs ? `?${qs}` : ""}`, {
      method: "PUT",
      credentials: "same-origin",
      headers: { "Content-Type": "application/octet-stream" },
      body: new Blob([data as BlobPart]),
    });
    if (res.status === 409) return { ok: false, conflict: true };
    if (!res.ok) return { ok: false, conflict: false };
    const body = (await res.json().catch(() => null)) as { sha256?: string } | null;
    return { ok: true, sha: body?.sha256 ?? "" };
  } catch {
    return { ok: false, conflict: false };
  }
}

export type SRAMStoreResult = "sent" | "unchanged" | "failed" | "conflict" | "held";

/*
 * SRAMSync keeps the game's own save in step with the server's.
 *
 * Each upload is based on the save this page loaded, or on the one it last
 * sent, so a save another player made in the meantime is refused by the
 * server rather than replaced. After a refusal nothing more is sent: every
 * later upload would be refused the same way. Uploads go one at a time, the
 * second based on the first, because the interval and leaving can overlap.
 */
export class SRAMSync {
  private base: string;
  private last: Uint8Array | null;
  private held = false;
  private queue: Promise<unknown> = Promise.resolve();
  private readonly itemID: number;

  constructor(itemID: number, loaded: FetchedSave | null) {
    this.itemID = itemID;
    this.base = loaded?.sha ?? NO_SAVE;
    this.last = loaded?.bytes ?? null;
  }

  /** Whether this page has stopped sending the game's save. */
  get stopped(): boolean {
    return this.held;
  }

  /** Sends the game's save when it differs from the last one. */
  store(now: Uint8Array | null): Promise<SRAMStoreResult> {
    const run = this.queue.then(() => this.send(now));
    this.queue = run.catch(() => {});
    return run;
  }

  private async send(now: Uint8Array | null): Promise<SRAMStoreResult> {
    if (this.held) return "held";
    if (!now || now.length === 0 || sameBytes(now, this.last)) return "unchanged";
    const r = await putSave(this.itemID, "sram", now, { basedOn: this.base });
    if (!r.ok) {
      if (r.conflict) this.held = true;
      return r.conflict ? "conflict" : "failed";
    }
    if (!SHA256.test(r.sha)) {
      // Stored, but with no checksum to base the next one on. Sending the
      // next one unconditionally is what this exists to stop.
      this.held = true;
      return "sent";
    }
    this.base = r.sha;
    this.last = now;
    return "sent";
  }
}

export function sameBytes(a: Uint8Array, b: Uint8Array | null): boolean {
  if (!b || a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
  return true;
}
