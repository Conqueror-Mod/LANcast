import { useAuthStatus } from "@/api/hooks";
import { readDevice, useDevice } from "./device";

/*
 * What this device has asked the server for.
 *
 * The honest limitation, stated first: once a download starts, the browser owns
 * it. There is no progress to read, no way to cancel it from here, and no way
 * to know whether it finished — a page that drew a progress bar would be
 * drawing a guess. So this is not a transfer manager. It is a **receipt list**:
 * what you asked for, when, and a link to ask again.
 *
 * That is still worth having. "Did I already pull that episode down?" and "what
 * was that film called" are the two questions a downloads page actually gets,
 * and both are answerable from the receipt alone.
 *
 * Per device, in localStorage, for the same reason the other device settings
 * are: the phone that downloaded something is the phone that has the file, and
 * a shared list would tell every other device it had a copy it does not have.
 *
 * And per account on that device (R13-8). One key for the device showed a
 * child signing in on the family laptop every title a parent had downloaded,
 * past their rating ceiling and their library limits, and marked those items
 * "downloaded" on Detail. Signing out cleared the query cache and left this.
 * The per-device argument covers which device has the file, not which person
 * may see the list. Receipts from before this change sit under the bare key,
 * which only a server with no accounts still reads: whose they were cannot be
 * known, so they are not handed to anybody.
 */

export const DOWNLOADS_KEY = "lancast:downloads";

/** The storage key for one account's receipts; the bare key with no account. */
export function downloadsKey(userId?: string | null): string {
  return userId ? `${DOWNLOADS_KEY}:${userId}` : DOWNLOADS_KEY;
}

export interface Receipt {
  itemId: number;
  title: string;
  /** The filename the server proposed, so the list matches what is on disk. */
  filename: string;
  /** Subtitle line — series and episode, or the year. */
  detail?: string;
  bytes?: number;
  at: number;
  /**
   * The paired server the file came from, by fingerprint, for a download
   * from a friend's share. Absent for this server's own. An id from another
   * server names a different item here, so the two are never confused.
   */
  peer?: string;
}

/** Where a receipt's title opens, on this server or on the friend's. */
export function receiptLink(r: Receipt): string {
  return r.peer
    ? `/peers/${encodeURIComponent(r.peer)}/item/${r.itemId}`
    : `/item/${r.itemId}`;
}

/** Where a receipt's file is asked for again. */
export function receiptURL(r: Receipt): string {
  return r.peer
    ? `/api/peers/${encodeURIComponent(r.peer)}/download?item=${r.itemId}`
    : downloadURL(r.itemId);
}

const sameItem = (a: Receipt, b: Receipt) =>
  a.itemId === b.itemId && (a.peer ?? "") === (b.peer ?? "");

// Enough that the answer to "did I get this already" is reliable, few enough
// that the list stays a page rather than an archive.
const LIMIT = 200;

/*
 * Adding a receipt, as a function of the list rather than of a hook.
 *
 * One receipt per item: downloading the same thing twice is a repeat, not a
 * second file, and a list that grew a row each time would bury everything else
 * under whichever title somebody had trouble with. That rule is the only
 * behaviour here worth getting wrong, so it is separated from the storage and
 * tested directly.
 */
export function recordReceipt(list: Receipt[], r: Receipt): Receipt[] {
  return [r, ...list.filter((x) => !sameItem(x, r))].slice(0, LIMIT);
}

// Until the server has said who is signed in, nobody's list is shown and
// nothing is recorded: guessing would be guessing somebody else's.
const UNKNOWN = `${DOWNLOADS_KEY}:unknown`;

export function useDownloads(): [Receipt[], (r: Receipt) => void, () => void] {
  const { data } = useAuthStatus();
  const key = data ? downloadsKey(data.user?.id) : UNKNOWN;
  const [list, set] = useDevice<Receipt[]>(key, []);
  if (key === UNKNOWN) return [[], () => {}, () => {}];
  return [list, (r) => set(recordReceipt(list, r)), () => set([])];
}

export function downloadedAt(
  itemId: number,
  userId?: string | null,
): number | undefined {
  // This server's own items only: a friend's item 7 is not our item 7.
  return readDevice<Receipt[]>(downloadsKey(userId), []).find(
    (r) => r.itemId === itemId && !r.peer,
  )?.at;
}

export function downloadURL(itemId: number): string {
  return `/api/items/${itemId}/download`;
}

/*
 * Start a download from a friend's server and record it.
 *
 * An anchor with the download attribute, clicked: the window's own download
 * manager takes it and the page stays where it is. Proven in a real WebView2
 * window, like location.href; an anchor also never looks like leaving the
 * page to anything listening for that.
 */
export function startPeerDownload(
  fingerprint: string,
  itemId: number,
  title: string,
  record: (r: Receipt) => void,
): void {
  const r: Receipt = { itemId, title, filename: "", peer: fingerprint, at: Date.now() };
  record(r);
  const a = document.createElement("a");
  a.href = receiptURL(r);
  a.download = "";
  a.rel = "noopener";
  document.body.append(a);
  a.click();
  a.remove();
}
