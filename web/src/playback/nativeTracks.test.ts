import { describe, it, expect } from "vitest";
import { activeCues, mpvAudioTrack, parseVTT } from "./nativeTracks";

describe("mpvAudioTrack", () => {
  const streams = [
    { index: 0, kind: "video" },
    { index: 1, kind: "audio" }, // English DTS
    { index: 2, kind: "subtitle" },
    { index: 3, kind: "audio" }, // commentary
  ];

  it("counts audio streams only, from 1, in file order", () => {
    expect(mpvAudioTrack(streams, 1)).toBe(1);
    expect(mpvAudioTrack(streams, 3)).toBe(2);
  });

  it("does not trust the list's order", () => {
    expect(mpvAudioTrack([...streams].reverse(), 3)).toBe(2);
  });

  it("leaves mpv alone for no choice or a stream that is not audio", () => {
    expect(mpvAudioTrack(streams, null)).toBeNull();
    expect(mpvAudioTrack(streams, 2)).toBeNull();
    expect(mpvAudioTrack(undefined, 1)).toBeNull();
  });
});

describe("parseVTT", () => {
  const vtt = `WEBVTT

1
00:00:01.000 --> 00:00:02.500 line:90%
<i>Hello</i> &amp; welcome

00:01:02.250 --> 00:01:04.000
Two
lines

bad --> cue

00:00:05.000 --> 00:00:04.000
Ends before it starts
`;

  it("reads timings and text, strips markup, decodes entities", () => {
    const cues = parseVTT(vtt);
    expect(cues).toEqual([
      { start: 1, end: 2.5, text: "Hello & welcome" },
      { start: 62.25, end: 64, text: "Two\nlines" },
    ]);
  });

  it("handles CRLF files", () => {
    expect(parseVTT(vtt.replace(/\n/g, "\r\n"))).toHaveLength(2);
  });
});

describe("activeCues", () => {
  const cues = [
    { start: 1, end: 2.5, text: "a" },
    { start: 2, end: 3, text: "b" },
  ];

  it("returns every cue covering the time, end exclusive", () => {
    expect(activeCues(cues, 2.2, 0).map((c) => c.text)).toEqual(["a", "b"]);
    expect(activeCues(cues, 2.5, 0).map((c) => c.text)).toEqual(["b"]);
    expect(activeCues(cues, 3, 0)).toEqual([]);
  });

  it("a positive offset shows cues later, as the element path does", () => {
    expect(activeCues(cues, 1.5, 1)).toEqual([]);
    expect(activeCues(cues, 2.5, 1).map((c) => c.text)).toEqual(["a"]);
  });
});
