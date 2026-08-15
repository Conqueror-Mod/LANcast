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
 */

export const DOWNLOADS_KEY = "lancast:downloads";

export interface Receipt {
  itemId: number;
  title: string;
  /** The filename the server proposed, so the list matches what is on disk. */
  filename: string;
  /** Subtitle line — series and episode, or the year. */
  detail?: string;
  bytes?: number;
  at: number;
}

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
  return [r, ...list.filter((x) => x.itemId !== r.itemId)].slice(0, LIMIT);
}

export function useDownloads(): [Receipt[], (r: Receipt) => void, () => void] {
  const [list, set] = useDevice<Receipt[]>(DOWNLOADS_KEY, []);
  return [list, (r) => set(recordReceipt(list, r)), () => set([])];
}

export function downloadedAt(itemId: number): number | undefined {
  return readDevice<Receipt[]>(DOWNLOADS_KEY, []).find(
    (r) => r.itemId === itemId,
  )?.at;
}

export function downloadURL(itemId: number): string {
  return `/api/items/${itemId}/download`;
}
