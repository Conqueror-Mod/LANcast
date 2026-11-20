import { beforeEach, describe, expect, it } from "vitest";
import { writeDevice } from "./device";
import {
  DOWNLOADS_KEY,
  downloadsKey,
  downloadURL,
  downloadedAt,
  recordReceipt,
  receiptLink,
  receiptURL,
  type Receipt,
} from "./downloads";

const receipt = (itemId: number, title: string): Receipt => ({
  itemId,
  title,
  filename: `${title}.mkv`,
  at: Date.now(),
});

beforeEach(() => {
  localStorage.clear();
  // The device store caches parsed values, so clearing localStorage alone would
  // leave the previous test's answer in place.
  writeDevice<Receipt[]>(DOWNLOADS_KEY, []);
});

describe("download receipts", () => {
  it("records newest first", () => {
    let list: Receipt[] = [];
    list = recordReceipt(list, receipt(1, "First"));
    list = recordReceipt(list, receipt(2, "Second"));
    expect(list.map((r) => r.itemId)).toEqual([2, 1]);
  });

  /*
   * Downloading the same thing twice is a repeat, not a second file.
   *
   * A list that grew a row each time would bury everything else under whichever
   * title somebody had trouble with — and the question this page answers is
   * "did I already get this", which one row answers and forty do not.
   */
  it("keeps one receipt per item, moved back to the top", () => {
    let list: Receipt[] = [];
    list = recordReceipt(list, receipt(1, "Arrival"));
    list = recordReceipt(list, receipt(2, "Other"));
    list = recordReceipt(list, receipt(1, "Arrival"));
    expect(list).toHaveLength(2);
    expect(list[0].itemId).toBe(1);
  });

  // A friend's item 1 is not our item 1: both keep their own receipt, and
  // each opens and downloads from the server it came from.
  it("keeps a friend's download apart from this server's own", () => {
    const FP = "BBBB4G6XEG33AUPJU7LGSTT4N74U6D4B";
    let list: Receipt[] = [];
    list = recordReceipt(list, receipt(1, "Ours"));
    list = recordReceipt(list, { ...receipt(1, "Theirs"), peer: FP });
    expect(list).toHaveLength(2);
    expect(receiptLink(list[0])).toBe(`/peers/${FP}/item/1`);
    expect(receiptURL(list[0])).toBe(`/api/peers/${FP}/download?item=1`);
    expect(receiptLink(list[1])).toBe("/item/1");
    expect(receiptURL(list[1])).toBe(downloadURL(1));

    writeDevice<Receipt[]>(DOWNLOADS_KEY, [{ ...receipt(3, "Theirs"), peer: FP }]);
    expect(downloadedAt(3)).toBeUndefined();
  });

  it("is a device setting, so it survives a reload", () => {
    writeDevice<Receipt[]>(DOWNLOADS_KEY, [receipt(7, "Kept")]);
    expect(JSON.parse(localStorage.getItem(DOWNLOADS_KEY)!)).toHaveLength(1);
    expect(downloadedAt(7)).toBeDefined();
    expect(downloadedAt(8)).toBeUndefined();
  });

  // Each account on the device keeps its own list (R13-8).
  it("keys the receipts by account", () => {
    expect(downloadsKey("u_1")).not.toBe(downloadsKey("u_2"));
    expect(downloadsKey(undefined)).toBe(DOWNLOADS_KEY);
    writeDevice<Receipt[]>(downloadsKey("u_1"), [receipt(9, "Mine")]);
    expect(downloadedAt(9, "u_1")).toBeDefined();
    expect(downloadedAt(9, "u_2")).toBeUndefined();
  });

  // The one URL mistake worth a test: a stream plays in a tab and a download
  // saves a file, and they differ by this suffix alone.
  it("points at the download route, never the stream", () => {
    expect(downloadURL(42)).toBe("/api/items/42/download");
  });
});
