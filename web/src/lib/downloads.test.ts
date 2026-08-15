import { beforeEach, describe, expect, it } from "vitest";
import { writeDevice } from "./device";
import {
  DOWNLOADS_KEY,
  downloadURL,
  downloadedAt,
  recordReceipt,
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

  it("is a device setting, so it survives a reload", () => {
    writeDevice<Receipt[]>(DOWNLOADS_KEY, [receipt(7, "Kept")]);
    expect(JSON.parse(localStorage.getItem(DOWNLOADS_KEY)!)).toHaveLength(1);
    expect(downloadedAt(7)).toBeDefined();
    expect(downloadedAt(8)).toBeUndefined();
  });

  // The one URL mistake worth a test: a stream plays in a tab and a download
  // saves a file, and they differ by this suffix alone.
  it("points at the download route, never the stream", () => {
    expect(downloadURL(42)).toBe("/api/items/42/download");
  });
});
