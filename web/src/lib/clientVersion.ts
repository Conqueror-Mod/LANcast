import { plainVersion } from "@/components/UpdateSettings";

/*
 * Is the window older than the server it is talking to?
 *
 * The in-app updater replaces the server and the web assets it serves. It
 * cannot replace a running client — a process cannot overwrite the executable
 * it is executing and keep going — so after a release that changed
 * LANcast-Client.exe, the app on screen is the previous version and nothing
 * said so. That was found the hard way: a fullscreen fix that lived in the
 * client shipped, the server updated itself, and the button went on doing
 * nothing because the window was 26 minutes older than the binary on disk.
 *
 * Only the desktop client has a version to compare. In a browser the page *is*
 * the client, and it arrived from the server it is asking about.
 */

/** What the desktop shell reports about itself, when there is one. */
export type DesktopVersion = { client_version?: string } | null;

/**
 * True when both versions are known and differ.
 *
 * Unknown on either side is not a mismatch. A development build reports "dev"
 * against a released server, which is a difference that means nothing and would
 * nag whoever is working on the thing. Same for a client too old to report a
 * version at all: it predates this check, and telling somebody to restart on
 * the strength of a missing field is a guess.
 */
export function clientIsStale(
  desktop: DesktopVersion,
  serverVersion: string | undefined,
): boolean {
  const client = plainVersion(desktop?.client_version);
  const server = plainVersion(serverVersion);
  if (!client || !server) return false;
  if (client === "dev" || server === "dev") return false;
  return client !== server;
}
