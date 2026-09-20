import { apiGet } from "@/api/client";

/*
 * Telling the desktop client which server it is actually talking to
 * (ADR 0070, as amended).
 *
 * The client pins the TLS serving key so the window can load at all, and that
 * key is not an identity: a serving certificate is regenerated whenever its
 * file is missing or corrupt, an operator may rotate a supplied one, and
 * deleting the certificate is this project's own documented repair for stale
 * SANs. Treating a change in it as an impostor spends the strongest warning
 * the client has on routine maintenance.
 *
 * The durable answer is the ADR 0044 identity, and it can only be read from
 * inside: `GET /api/identity` is session-gated, so nothing outside an
 * authenticated page can reach it. That is why this lives in the page and
 * hands the value out through a binding rather than the client fetching it
 * for itself -- the session is here, in the web view, and nowhere else.
 *
 * It is a report, not a decision. The process decides what to do with it, and
 * refuses to overwrite an identity it already holds, because a page that could
 * replace one could replace it with an attacker's.
 */

declare global {
  interface Window {
    /** Records this server's identity against the address being used. */
    lancastServerIdentity?: (fingerprint: string) => Promise<{ ok?: boolean; error?: string }>;
  }
}

/** Reported once per page load; the identity of a running server cannot change. */
let reported = false;

/**
 * reportServerIdentity tells the desktop client who this server is, when
 * running inside one and signed in.
 *
 * Silent on every failure. It runs behind an authenticated page doing
 * something else, and a browser tab, an older client, or a server that will
 * not answer are all ordinary — none of them is worth putting in front of
 * somebody who came here to watch something.
 */
export async function reportServerIdentity(): Promise<void> {
  if (reported || typeof window.lancastServerIdentity !== "function") return;
  reported = true;
  try {
    const { fingerprint } = await apiGet<{ fingerprint: string }>("/api/identity");
    if (fingerprint) await window.lancastServerIdentity(fingerprint);
  } catch {
    // See above. Nothing here is worth interrupting anyone for.
  }
}

/** Test seam: forget that this page has already reported. */
export function resetServerIdentityReport(): void {
  reported = false;
}
