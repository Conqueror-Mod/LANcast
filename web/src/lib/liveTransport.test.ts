import { describe, expect, it } from "vitest";

import {
  FMP4_H264_AAC,
  LIVE_TRANSPORT_DEFAULT,
  livePath,
  type MediaCapability,
} from "./liveTransport";

/*
 * These are about the *choice*, not about playback.
 *
 * jsdom has no MediaSource and performs no media, so nothing here can prove a
 * channel plays — that is what the harness and the running app are for. What it
 * can prove is that a device which cannot do MSE is never sent down that path,
 * which is the failure that would present as a black rectangle rather than as
 * an error.
 */

const chromium: MediaCapability = {
  hasMediaSource: true,
  isTypeSupported: (t) => t === FMP4_H264_AAC,
  canPlayType: () => "", // no native HLS
};

const safari: MediaCapability = {
  hasMediaSource: true,
  isTypeSupported: () => true,
  canPlayType: () => "probably", // native HLS
};

const ancient: MediaCapability = {
  hasMediaSource: false,
  isTypeSupported: () => false,
  canPlayType: () => "",
};

describe("livePath", () => {
  it("defaults to the transport this client shipped with", () => {
    expect(LIVE_TRANSPORT_DEFAULT).toBe("progressive");
  });

  it("leaves the old path alone when the setting is off, whatever the device can do", () => {
    expect(livePath("progressive", chromium)).toBe("progressive");
    expect(livePath("progressive", safari)).toBe("progressive");
  });

  it("uses MSE on a browser that has it and lacks native HLS", () => {
    expect(livePath("mse", chromium)).toBe("mse");
  });

  it("gives a native-HLS browser the playlist rather than a pipeline to it", () => {
    // Safari plays the playlist itself; handing it MSE builds a road to
    // somewhere it already is.
    expect(livePath("mse", safari)).toBe("native-hls");
  });

  it("falls back rather than failing when MediaSource is absent", () => {
    // The setting asks; it cannot force. A dead channel is a worse outcome
    // than an ignored preference.
    expect(livePath("mse", ancient)).toBe("progressive");
  });

  it("falls back when MediaSource exists but refuses the codecs a channel uses", () => {
    const picky: MediaCapability = {
      hasMediaSource: true,
      isTypeSupported: () => false,
      canPlayType: () => "",
    };
    expect(livePath("mse", picky)).toBe("progressive");
  });
});
