import { describe, expect, it } from "vitest";

import { conversionHelp } from "./conversionAvailable";

/*
 * Explaining a server that cannot convert.
 *
 * ADR 0043 built the install button and ADR 0048 put the tools on the setup
 * form. Neither reaches a server already running without them, and neither
 * reaches the person most affected — a member, for whom the button is
 * admin-only and lives behind a settings page they cannot open.
 *
 * The original report was *"AC-3 is not supported yet"*: a wrong conclusion
 * about the software, reached because nothing said otherwise.
 */
describe("conversionHelp", () => {
  it("says nothing when the server can convert", () => {
    expect(conversionHelp(true, "admin", "file")).toBeNull();
  });

  /*
   * The direction that must not be got wrong.
   *
   * A client can be newer than its server, so `undefined` means "this server
   * does not report it". Reading that as incapable would put a warning in
   * front of somebody whose playback works perfectly — a worse error than
   * staying quiet, because it is wrong about a working system.
   */
  it("says nothing when the server does not report the capability at all", () => {
    expect(conversionHelp(undefined, "member", "file")).toBeNull();
  });

  it("says nothing about a file that plays as it is", () => {
    // Most of a library still direct-plays on a server with no ffmpeg. Warning
    // there would be noise on exactly the files that work.
    expect(conversionHelp(false, "admin", "no")).toBeNull();
  });

  /*
   * The whole reason the message is a function of the reader.
   *
   * Sending a member to Settings is worse than saying nothing: the page is
   * admin-only, so the instruction cannot be followed and the reader learns
   * only that the software is confusing as well as broken.
   */
  it("tells an admin where the button is", () => {
    const help = conversionHelp(false, "admin", "file")!;
    expect(help.action).toContain("Settings");
  });

  it("does not send a member to a page they cannot open", () => {
    const help = conversionHelp(false, "member", "file")!;
    expect(help.action).not.toContain("Settings → Metadata");
    expect(help.action).toContain("Ask whoever runs this server");
  });

  /*
   * Live TV is the harshest version of this failure and gets its own sentence.
   *
   * A library on a server with no ffmpeg looks mostly fine, because most films
   * direct-play. Every channel is an ffmpeg session, so Live TV is uniformly
   * dead — and "this file needs converting" is the wrong sentence for a screen
   * where nothing will ever play.
   */
  it("says something different about channels than about files", () => {
    const channel = conversionHelp(false, "admin", "channel")!;
    const file = conversionHelp(false, "admin", "file")!;
    expect(channel.action).toContain("Every channel");
    expect(channel.action).not.toBe(file.action);
  });

  it("names ffmpeg in both, because that is the thing to install", () => {
    expect(conversionHelp(false, "member", "file")!.action).toContain("ffmpeg");
    expect(conversionHelp(false, "member", "channel")!.action).toContain("ffmpeg");
  });
});
