/*
 * Reading a pairing invite, for display only.
 *
 * An invite is `lancast-invite-v1:` and base64url of a small JSON object
 * (internal/peer/invite.go). Three hundred characters of that is unreadable,
 * and reported as such: two invites differing by one address differ in a few
 * characters somewhere in the middle, and nobody could tell whether the one on
 * screen had changed, or whether a pasted one was the one they meant.
 *
 * So the client decodes it to say what it carries, and draws a short check
 * code from it that changes whenever any part does. **This is never
 * validation.** The server parses every invite it is given and is the only
 * judge of whether one is acceptable; a decode that fails here just means the
 * preview says nothing.
 */
const PREFIX = "lancast-invite-v1:";

export type InviteContents = {
  name: string;
  fingerprint: string;
  addrs: string[];
};

export function decodeInvite(raw: string): InviteContents | null {
  const s = raw.replace(/\s+/g, "");
  if (!s.startsWith(PREFIX)) return null;
  try {
    const b64 = s.slice(PREFIX.length).replace(/-/g, "+").replace(/_/g, "/");
    const padded = b64 + "=".repeat((4 - (b64.length % 4)) % 4);
    const bytes = Uint8Array.from(atob(padded), (c) => c.charCodeAt(0));
    const w = JSON.parse(new TextDecoder().decode(bytes)) as {
      fp?: unknown;
      name?: unknown;
      addrs?: unknown;
    };
    if (typeof w.fp !== "string") return null;
    return {
      name: typeof w.name === "string" ? w.name : "",
      fingerprint: w.fp,
      addrs: Array.isArray(w.addrs) ? w.addrs.filter((a): a is string => typeof a === "string") : [],
    };
  } catch {
    return null;
  }
}

// The fingerprint in groups of four, as the server's own display form is.
export function groupFingerprint(fp: string): string {
  return fp.toUpperCase().replace(/(.{4})(?=.)/g, "$1-");
}

/*
 * A short code standing for the whole invite: FNV-1a over its characters,
 * written as two groups of four in an alphabet with no look-alikes. Not a
 * security property — the fingerprint is that — only a way to see at a glance
 * that two long strings are, or are not, the same one.
 */
const ALPHABET = "23456789ABCDEFGHJKMNPQRSTUVWXYZ";

export function inviteCode(raw: string): string {
  const s = raw.replace(/\s+/g, "");
  // Two passes with different starting points, four characters from each.
  return `${group(fnv(s, 0x811c9dc5))}-${group(fnv(s, 0x01000193))}`;
}

function fnv(s: string, seed: number): number {
  let h = seed >>> 0;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 0x01000193) >>> 0;
  }
  return h;
}

function group(h: number): string {
  let out = "";
  for (let i = 0; i < 4; i++) {
    out += ALPHABET[h % ALPHABET.length];
    h = Math.floor(h / ALPHABET.length);
  }
  return out;
}
