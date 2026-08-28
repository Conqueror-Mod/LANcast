/*
 * What to say when the server cannot convert anything.
 *
 * ADR 0043 gave LANcast an install button and ADR 0048 put the tools on the
 * setup form. Neither reaches a server that is already running without them —
 * and neither reaches the person most affected, who is a **member**: the button
 * is admin-only and lives in Settings, which they cannot open.
 *
 * The failure this produces is not a missing feature. It is somebody watching a
 * black rectangle and concluding the software cannot play their files, from a
 * symptom two layers from its cause. The original report that started this
 * whole line of work was *"AC-3 is not supported yet"* — a wrong conclusion
 * about the software, reached because nothing said otherwise.
 *
 * So the rule here is that the message differs by what the reader can actually
 * do. Telling a member to visit an admin-only settings page is worse than
 * saying nothing: it sends them somewhere they cannot go.
 */

/*
 * Whether the thing about to play needs converting at all.
 *
 * `channel` is its own case rather than a boolean, because Live TV is the
 * harshest version of this failure: a film that direct-plays still works on a
 * server without ffmpeg, so a library looks mostly fine, while *every* channel
 * is an ffmpeg session and Live TV is uniformly dead.
 */
export type Needs = "no" | "file" | "channel";

export type ConversionHelp = {
  /** One line naming the problem in the reader's terms. */
  title: string;
  /** What this particular person can do about it. */
  action: string;
};

/**
 * conversionHelp explains a server that cannot convert, or null when there is
 * nothing to explain.
 *
 * `canConvert` is undefined on a server too old to report it. That reads as
 * *capable* rather than as broken: a client newer than its server is ordinary
 * here, and putting a warning in front of somebody whose playback works is a
 * worse error than staying quiet.
 */
export function conversionHelp(
  canConvert: boolean | undefined,
  role: string | undefined,
  needs: Needs,
): ConversionHelp | null {
  if (canConvert !== false) return null;
  // A file that plays as it is does not care whether the server can convert,
  // and warning about it would be noise on the files that work.
  if (needs === "no") return null;

  /*
   * The two cases differ in what the reader is looking at, so they get
   * different sentences rather than one sentence edited afterwards. A message
   * assembled by string surgery at the call site stops matching the moment
   * somebody rewords the original, and does so silently.
   */
  const what =
    needs === "channel"
      ? "Every channel is converted as it plays, and ffmpeg is not installed."
      : "This file needs converting before a browser can play it, and ffmpeg is not installed.";

  const fix =
    role === "admin"
      ? "Install it from Settings → Metadata, then try again."
      : "Ask whoever runs this server to install it — it is a one-click download in their settings.";

  return {
    title: "This server cannot convert video yet",
    action: `${what} ${fix}`,
  };
}
