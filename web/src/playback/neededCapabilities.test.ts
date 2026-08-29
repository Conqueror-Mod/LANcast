import { describe, expect, it } from "vitest";

import { capabilitiesNeededBy } from "./capabilities";

/*
 * Which claims a failed direct play is allowed to withdraw.
 *
 * The old rule withdrew every claim this browser makes, because the element's
 * error does not say which codec let it down. One TrueHD file that no browser
 * can decode therefore took hevc, hevc10, high10, ac3, eac3, flacmp4 and
 * opusmp4 with it — measured on a real install as 130 transcode sessions whose
 * stated reason was `video codec hevc is not supported`, on a machine that
 * decodes HEVC in hardware.
 *
 * A file cannot be ruined by a claim it never needed. These pin that down, and
 * particularly the case that caused the damage: a file whose only unusual
 * stream is one nothing here models must cost nothing.
 */

const v = (codec: string, profile?: string) => ({
  kind: "video",
  codec,
  profile,
});
const a = (codec: string) => ({ kind: "audio", codec });

describe("what a file could have been ruined by", () => {
  /*
   * The file that caused the fault, and the whole point of the change.
   *
   * TrueHD is in no browser's baseline and is not a claim this client makes.
   * A direct play of it fails, and none of the seven claims had anything to do
   * with it.
   */
  it("blames nothing for a codec no claim covers", () => {
    expect(capabilitiesNeededBy([v("h264", "High"), a("truehd")])).toEqual([]);
    expect(capabilitiesNeededBy([v("h264", "High"), a("dts")])).toEqual([]);
  });

  it("blames only the video claim an 8-bit HEVC file used", () => {
    // Not hevc10: an 8-bit file cannot have been spoiled by a permission for a
    // bit depth it does not have.
    expect(capabilitiesNeededBy([v("hevc", "Main"), a("aac")])).toEqual([
      "hevc",
    ]);
  });

  it("blames both HEVC claims on a Main 10 file", () => {
    expect(capabilitiesNeededBy([v("hevc", "Main 10")])).toEqual([
      "hevc",
      "hevc10",
    ]);
  });

  it("blames high10 only for ten-bit H.264", () => {
    expect(capabilitiesNeededBy([v("h264", "High 10")])).toEqual(["high10"]);
    expect(capabilitiesNeededBy([v("h264", "High")])).toEqual([]);
  });

  it("blames the audio claim the file actually carries", () => {
    expect(capabilitiesNeededBy([v("h264", "High"), a("ac3")])).toEqual(["ac3"]);
    expect(capabilitiesNeededBy([v("h264", "High"), a("eac3")])).toEqual([
      "eac3",
    ]);
  });

  it("maps FLAC and Opus to the in-MP4 permissions, which is the question asked", () => {
    expect(capabilitiesNeededBy([a("flac")])).toEqual(["flacmp4"]);
    expect(capabilitiesNeededBy([a("opus")])).toEqual(["opusmp4"]);
  });

  it("blames video and audio together when the file uses both", () => {
    expect(capabilitiesNeededBy([v("hevc", "Main 10"), a("eac3")])).toEqual([
      "eac3",
      "hevc",
      "hevc10",
    ]);
  });

  /*
   * An unprobed file blames nothing rather than everything.
   *
   * Blaming everything is what this replaced, and doing it whenever the streams
   * are unknown would reintroduce the fault through the one door left open.
   * The recovery does not depend on it: the file is converted either way.
   */
  it("blames nothing when the streams are unknown", () => {
    expect(capabilitiesNeededBy(undefined)).toEqual([]);
    expect(capabilitiesNeededBy([])).toEqual([]);
  });

  it("ignores subtitle streams, which no claim covers", () => {
    expect(
      capabilitiesNeededBy([{ kind: "subtitle", codec: "subrip" }]),
    ).toEqual([]);
  });
});
