/*
 * How to reach the person who makes LANcast.
 *
 * A mail link, not a form that posts somewhere. LANcast does not phone home
 * (README, principle four), and a "report a problem" button that sent a request
 * to a server of ours would be the first thing in the program that did. A
 * mailto hands the message to the mail program the person already uses: they
 * see every word before it goes, and nothing leaves the machine until they
 * press Send themselves.
 *
 * The version goes in the body because it is the first question every report
 * gets asked, and the person filing it should not have to go and find it.
 */
export const DEVELOPER_NAME = "Chris Bowles";
export const DEVELOPER_EMAIL = "conqueror.conqueredtime@gmail.com";
export const DONATE_URL = "https://ko-fi.com/christheconqueror";

export function reportIssueHref(version?: string): string {
  const subject = version ? `LANcast ${version}: problem report` : "LANcast: problem report";
  const body = [
    "What happened:",
    "",
    "",
    "What you expected:",
    "",
    "",
    "Steps to make it happen again:",
    "",
    "",
    "---",
    `LANcast version: ${version || "unknown"}`,
  ].join("\n");
  return `mailto:${DEVELOPER_EMAIL}?subject=${encodeURIComponent(subject)}&body=${encodeURIComponent(body)}`;
}
