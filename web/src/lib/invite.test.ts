/*
 * Reading an invite for display. Reported: invites are a long string that
 * changes in a few characters, so nobody could tell whether the one on screen
 * had been updated, or whether a pasted one was the one they meant.
 */
import { describe, it, expect } from "vitest";
import { decodeInvite, groupFingerprint, inviteCode } from "./invite";

// The server's format: prefix + base64url (no padding) of the wire JSON.
function encode(w: object): string {
  const b64 = btoa(JSON.stringify(w)).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
  return "lancast-invite-v1:" + b64;
}

const FP = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567ABCDEFGHIJKLMNOPQRST";

describe("decodeInvite", () => {
  it("says what an invite carries", () => {
    const inv = encode({ v: 1, fp: FP, name: "Aither", addrs: ["192.168.1.10:8443", "[fe80::1]:8443"] });
    expect(decodeInvite(inv)).toEqual({
      name: "Aither",
      fingerprint: FP,
      addrs: ["192.168.1.10:8443", "[fe80::1]:8443"],
    });
  });

  it("survives the whitespace a chat window adds", () => {
    const inv = encode({ v: 1, fp: FP, name: "Aither", addrs: ["a:1"] });
    const wrapped = `  ${inv.slice(0, 40)}\n${inv.slice(40)}  `;
    expect(decodeInvite(wrapped)?.name).toBe("Aither");
  });

  it("answers null for anything that is not an invite, rather than throwing", () => {
    expect(decodeInvite("hello")).toBeNull();
    expect(decodeInvite("lancast-invite-v1:!!!")).toBeNull();
    expect(decodeInvite("lancast-invite-v1:" + btoa("{}"))).toBeNull();
  });
});

describe("inviteCode", () => {
  it("is stable for the same invite, ignoring whitespace", () => {
    const inv = encode({ v: 1, fp: FP, name: "A", addrs: ["10.0.0.1:8443"] });
    expect(inviteCode(inv)).toBe(inviteCode(` ${inv}\n`));
    expect(inviteCode(inv)).toMatch(/^[2-9A-Z]{4}-[2-9A-Z]{4}$/);
  });

  it("changes visibly when one address changes", () => {
    const a = encode({ v: 1, fp: FP, name: "A", addrs: ["10.0.0.1:8443"] });
    const b = encode({ v: 1, fp: FP, name: "A", addrs: ["10.0.0.2:8443"] });
    const ca = inviteCode(a);
    const cb = inviteCode(b);
    expect(ca).not.toBe(cb);
    // Not merely one character: each half comes from its own pass.
    expect(ca.slice(0, 4)).not.toBe(cb.slice(0, 4));
    expect(ca.slice(5)).not.toBe(cb.slice(5));
  });
});

it("groups a fingerprint in fours, as the server displays it", () => {
  expect(groupFingerprint("abcdefghij")).toBe("ABCD-EFGH-IJ");
});
