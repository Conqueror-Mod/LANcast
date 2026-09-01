/*
 * Sizes on the face-model consent list.
 *
 * Reported: the detector showed as "0 MB". It is 232,589 bytes, and the
 * formatter rounded everything to whole megabytes — so the screen that exists
 * to say what is about to be downloaded described one of the three files as
 * nothing.
 *
 * The sizes below are the real pinned ones from internal/faceinstall/install.go
 * rather than invented round numbers, because the bug was specifically about a
 * value that rounds to zero and an invented fixture would have been chosen from
 * the range that already worked.
 */
import { describe, it, expect } from "vitest";
import { formatBytes } from "./Settings";

const YUNET = 232589; // face_detection_yunet_2023mar.onnx
const SFACE = 38696353; // face_recognition_sface_2021dec.onnx
const RUNTIME = 79645520; // onnxruntime-win-x64-1.29.0.zip

describe("sizes on the consent list", () => {
  // The reported fault.
  it("does not describe the detector as zero", () => {
    expect(formatBytes(YUNET)).toBe("227 KB");
  });

  it("still reports the large assets in megabytes", () => {
    expect(formatBytes(SFACE)).toBe("37 MB");
    expect(formatBytes(RUNTIME)).toBe("76 MB");
  });

  it("reports the total the three of them come to", () => {
    expect(formatBytes(YUNET + SFACE + RUNTIME)).toBe("113 MB");
  });

  /*
   * The general property, which is what stops this returning under a different
   * asset: nothing with bytes in it may render as zero.
   */
  it("never renders a non-empty file as zero", () => {
    for (const n of [1, 512, 1023, 1024, 51200, 1048575, 1048576]) {
      expect(formatBytes(n), `${n} bytes`).not.toMatch(/^0\b/);
    }
  });

  // "1024 KB" is a size nobody writes; just under a megabyte has to cross over.
  it("crosses to megabytes rather than printing 1024 KB", () => {
    expect(formatBytes(1024000)).toBe("1 MB");
    expect(formatBytes(1048575)).toBe("1 MB");
  });

  it("uses bytes below a kilobyte", () => {
    expect(formatBytes(1)).toBe("1 bytes");
    expect(formatBytes(999)).toBe("999 bytes");
  });

  // A missing size arrives as 0 from `?? 0`, and must not crash or read oddly.
  it("handles an absent size", () => {
    expect(formatBytes(0)).toBe("0 bytes");
    expect(formatBytes(-5)).toBe("0 bytes");
    expect(formatBytes(NaN)).toBe("0 bytes");
  });
});
