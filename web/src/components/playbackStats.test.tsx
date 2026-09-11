/*
 * The statistics overlay, rendered.
 *
 * The arithmetic is tested beside stats.ts; this is the wiring — that the panel
 * actually reads the element it was handed, repeats the reading, and says so
 * when the browser will not answer.
 *
 * What this cannot cover, stated rather than implied: jsdom performs no layout,
 * so nothing here proves the panel is on screen, unobscured, or the right size.
 * A menu in this project passed every assertion about its contents for four
 * releases while being painted underneath the docked player. Looking at it is
 * still the only way to know it is visible.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";

import { PlaybackStats } from "./PlaybackStats";
import {
  forgetHLSIncidents,
  noteHLSIncident,
} from "@/playback/hlsIncident";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  vi.useFakeTimers();
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.useRealTimers();
  vi.restoreAllMocks();
  // The record outlives the component, which is the point of it — so it has to
  // be cleared between tests or one fallback leaks into the next assertion.
  forgetHLSIncidents();
});

/** A video element that reports whatever a test wants it to. */
function stubVideo(quality: () => { dropped: number; total: number }) {
  const el = document.createElement("video");
  Object.defineProperty(el, "getVideoPlaybackQuality", {
    value: () => ({
      droppedVideoFrames: quality().dropped,
      totalVideoFrames: quality().total,
    }),
    configurable: true,
  });
  Object.defineProperty(el, "videoWidth", { value: 1920, configurable: true });
  Object.defineProperty(el, "videoHeight", { value: 800, configurable: true });
  return el;
}

function render(video: HTMLVideoElement | null) {
  act(() => {
    root.render(<PlaybackStats video={video} onClose={() => {}} />);
  });
}

describe("the playback statistics panel", () => {
  it("reports what the element says as soon as it opens", () => {
    render(stubVideo(() => ({ dropped: 6, total: 1200 })));
    expect(host.textContent).toContain("6 of 1200 frames dropped");
    expect(host.textContent).toContain("1920×800");
  });

  /*
   * The assertion this panel exists for.
   *
   * A film played badly for an evening while every server-side number looked
   * healthy. This is the one reading that separates "frames are being thrown
   * away" from "frames are arriving unevenly", and it has to be legible as a
   * verdict rather than only as a number.
   */
  it("says in words when the picture is losing time", () => {
    render(stubVideo(() => ({ dropped: 300, total: 10000 })));
    expect(host.textContent).toContain("dropping frames");
  });

  it("stays quiet when the picture is healthy", () => {
    render(stubVideo(() => ({ dropped: 2, total: 10000 })));
    expect(host.textContent).not.toContain("dropping frames");
  });

  it("keeps reading as playback continues", () => {
    let total = 1000;
    render(stubVideo(() => ({ dropped: 0, total })));
    expect(host.textContent).toContain("of 1000 frames");

    total = 1024;
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(host.textContent).toContain("of 1024 frames");
    // A second sample is what makes a frame rate knowable at all.
    expect(host.textContent).toContain("24.0 fps");
  });

  /*
   * Firefox has never implemented getVideoPlaybackQuality.
   *
   * Reporting zeroes there would be the worst outcome available: a confident
   * "no frames dropped" built on the browser declining to answer.
   */
  it("admits when the browser will not report frame statistics", () => {
    render(document.createElement("video"));
    expect(host.textContent).toContain("does not report frame statistics");
    expect(host.textContent).not.toContain("0 of 0 frames dropped");
  });

  it("does not fall over before the element exists", () => {
    render(null);
    expect(host.textContent).toContain("Reading");
  });
});

/*
 * The fallback reaches the panel.
 *
 * This is the half that has been wrong every time in this project: the rules
 * were tested and what reached them was not. The record is only worth keeping
 * if somebody can read it, and the panel is the only place anybody can — the
 * desktop client has no console within reach.
 */
describe("why the segmented path was abandoned", () => {
  it("shows nothing when nothing has fallen back", () => {
    render(stubVideo(() => ({ dropped: 0, total: 100 })));
    act(() => vi.advanceTimersByTime(1100));

    expect(host.querySelector(".pstats__incident")).toBeNull();
  });

  it("shows the message the element gave, which is the finding", () => {
    noteHLSIncident({
      code: 4,
      message: "PipelineStatus::DEMUXER_ERROR_COULD_NOT_PARSE",
      readyState: 0,
      networkState: 3,
      buffered: 0,
      at: 1024,
      clock: 1,
      itemID: 7058,
      title: "The Fifth Element",
    });
    render(stubVideo(() => ({ dropped: 0, total: 100 })));
    act(() => vi.advanceTimersByTime(1100));

    const text = host.querySelector(".pstats__incident")?.textContent ?? "";
    expect(
      text,
      "the panel does not show why segments were given up on, so the message " +
        "is kept where nobody can read it",
    ).toContain("DEMUXER_ERROR_COULD_NOT_PARSE");
    expect(text).toContain("code 4");
    expect(text).toContain("ready 0");
  });

  /*
   * Picked up even when the panel was opened afterwards.
   *
   * A fallback happens in the first second of playback and the panel is opened
   * when somebody notices the picture is wrong, which is minutes later. A
   * record only readable if the panel was already open would miss every real
   * case.
   */
  it("is found by a panel opened after the fact", () => {
    render(stubVideo(() => ({ dropped: 0, total: 100 })));
    act(() => vi.advanceTimersByTime(1100));
    expect(host.querySelector(".pstats__incident")).toBeNull();

    noteHLSIncident({
      code: 4,
      message: "DEMUXER_ERROR_NO_SUPPORTED_STREAMS",
      readyState: 1,
      networkState: 3,
      buffered: 6,
      at: 0,
      clock: 2,
      itemID: 37106,
      title: "An Episode",
    });
    act(() => vi.advanceTimersByTime(1100));

    expect(host.querySelector(".pstats__incident")?.textContent ?? "").toContain(
      "DEMUXER_ERROR_NO_SUPPORTED_STREAMS",
    );
  });
});
