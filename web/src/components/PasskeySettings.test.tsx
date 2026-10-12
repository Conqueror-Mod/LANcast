/*
 * Passkeys in the client (ADR 0082): the conversion between the server's
 * base64url and the browser's buffers, the sign-in button appearing only where
 * a passkey can work, and the Account panel's add, rename and remove.
 *
 * navigator.credentials is stubbed: jsdom has no authenticator. What is proven
 * is the wiring — what is sent, what is shown — not that a phone signs.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { PasskeySettings } from "./PasskeySettings";
import { Login } from "@/screens/Auth";
import { fromB64url, toB64url } from "@/lib/webauthn";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let sent: { url: string; method: string; body: unknown }[];
let routes: Record<string, unknown>;

beforeEach(() => {
  sent = [];
  routes = {};
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      sent.push({ url, method, body: init?.body ? JSON.parse(String(init.body)) : undefined });
      const key = `${method} ${url}`;
      const body = key in routes ? routes[key] : {};
      if (body === 204) return new Response(null, { status: 204 });
      return new Response(JSON.stringify(body), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function settle() {
  for (let i = 0; i < 6; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
  }
}

async function render(node: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(<QueryClientProvider client={qc}>{node}</QueryClientProvider>);
  });
  await settle();
}

const button = (label: string) =>
  [...host.querySelectorAll("button")].find((b) => b.textContent === label) as
    | HTMLButtonElement
    | undefined;

function type(el: HTMLInputElement, value: string) {
  const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
  set.call(el, value);
  el.dispatchEvent(new Event("input", { bubbles: true }));
}

describe("base64url conversion", () => {
  it("round-trips bytes, including the ones base64 and base64url disagree on", () => {
    const bytes = new Uint8Array([0, 0xfb, 0xff, 0xfe, 1, 2, 3, 0x3e, 0x3f]);
    const s = toB64url(bytes);
    expect(s).not.toMatch(/[+/=]/);
    expect(new Uint8Array(fromB64url(s))).toEqual(bytes);
  });
  it("accepts padded input", () => {
    expect(new Uint8Array(fromB64url("AQI="))).toEqual(new Uint8Array([1, 2]));
  });
});

describe("sign-in screen", () => {
  it("offers no passkey button where passkeys cannot work", async () => {
    routes["GET /api/auth/status"] = { configured: true, authenticated: false, passkeys: { available: false } };
    await render(<Login />);
    expect(button("Sign in with a passkey")).toBeUndefined();
  });

  it("signs in with a passkey, sending the browser's answer as base64url", async () => {
    routes["GET /api/auth/status"] = {
      configured: true, authenticated: false,
      passkeys: { available: true, rp_id: "example.dynv6.net" },
    };
    routes["POST /api/auth/passkey/login/options"] = {
      challenge_id: "c1",
      public_key: { challenge: "AQID", rpId: "example.dynv6.net", userVerification: "required" },
    };
    routes["POST /api/auth/passkey/login"] = { authenticated: true, user: { id: "u_1", name: "chris", role: "admin" } };
    const get = vi.fn(async (opts: CredentialRequestOptions) => {
      expect(new Uint8Array(opts.publicKey!.challenge as ArrayBuffer)).toEqual(new Uint8Array([1, 2, 3]));
      return {
        rawId: new Uint8Array([9, 9]).buffer,
        response: {
          clientDataJSON: new Uint8Array([1]).buffer,
          authenticatorData: new Uint8Array([2]).buffer,
          signature: new Uint8Array([3]).buffer,
          userHandle: new TextEncoder().encode("u_1").buffer,
        },
      };
    });
    vi.stubGlobal("navigator", { ...navigator, credentials: { get } });
    await render(<Login />);
    await act(async () => button("Sign in with a passkey")!.click());
    await settle();
    expect(get).toHaveBeenCalledTimes(1);
    const finish = sent.find((s) => s.url === "/api/auth/passkey/login");
    expect(finish?.body).toEqual({
      challenge_id: "c1", id: "CQk", client_data_json: "AQ", authenticator_data: "Ag",
      signature: "Aw", user_handle: toB64url(new TextEncoder().encode("u_1")),
    });
  });

  it("says quietly that nothing happened when the person closes the prompt", async () => {
    routes["GET /api/auth/status"] = { configured: true, authenticated: false, passkeys: { available: true, rp_id: "x" } };
    routes["POST /api/auth/passkey/login/options"] = { challenge_id: "c", public_key: { challenge: "AQ" } };
    vi.stubGlobal("navigator", {
      ...navigator,
      credentials: { get: async () => { throw new DOMException("closed", "NotAllowedError"); } },
    });
    await render(<Login />);
    await act(async () => button("Sign in with a passkey")!.click());
    await settle();
    expect(host.querySelector('[role="alert"]')!.textContent).toContain("No passkey was used");
    expect(sent.some((s) => s.url === "/api/auth/passkey/login")).toBe(false);
  });
});

const listed = {
  id: "pk_1", name: "Phone", created_at: "2026-10-11T20:00:00Z",
  last_used_at: "2026-10-11T21:00:00Z", synced: true, rp_id: "example.dynv6.net", usable: true,
};

describe("Account passkeys", () => {
  it("lists passkeys anywhere, and says why it cannot add one off the public name", async () => {
    routes["GET /api/profile/passkeys"] = { passkeys: [listed], available_here: false };
    await render(<PasskeySettings />);
    expect(host.textContent).toContain("Phone");
    expect(host.textContent).toContain("synced across your devices");
    expect(host.textContent).toContain("only be added when LANcast is opened at its public name");
    expect(button("Add a passkey")).toBeUndefined();
  });

  it("explains a passkey made for an old name", async () => {
    routes["GET /api/profile/passkeys"] = {
      passkeys: [{ ...listed, rp_id: "old.duckdns.org", usable: false }], available_here: true,
    };
    await render(<PasskeySettings />);
    expect(host.textContent).toContain("Made for old.duckdns.org");
  });

  it("removes in two presses, never one", async () => {
    routes["GET /api/profile/passkeys"] = { passkeys: [listed], available_here: true };
    routes["DELETE /api/profile/passkeys/pk_1"] = 204;
    await render(<PasskeySettings />);
    await act(async () => button("Remove")!.click());
    expect(sent.some((s) => s.method === "DELETE")).toBe(false);
    await act(async () => button("Remove passkey")!.click());
    await settle();
    expect(sent.some((s) => s.method === "DELETE" && s.url === "/api/profile/passkeys/pk_1")).toBe(true);
  });

  it("adds a passkey: password first, then the device, then the answer", async () => {
    routes["GET /api/profile/passkeys"] = { passkeys: [], available_here: true };
    routes["POST /api/profile/passkeys/options"] = {
      challenge_id: "r1",
      public_key: {
        challenge: "BAU", rp: { id: "example.dynv6.net", name: "LANcast" },
        user: { id: toB64url(new TextEncoder().encode("u_1")), name: "chris", displayName: "chris" },
        excludeCredentials: [{ type: "public-key", id: "CQk" }],
      },
    };
    routes["POST /api/profile/passkeys"] = listed;
    const create = vi.fn(async (opts: CredentialCreationOptions) => {
      const pk = opts.publicKey!;
      expect(new TextDecoder().decode(pk.user.id as ArrayBuffer)).toBe("u_1");
      expect(new Uint8Array(pk.excludeCredentials![0].id as ArrayBuffer)).toEqual(new Uint8Array([9, 9]));
      return {
        rawId: new Uint8Array([7]).buffer,
        response: { clientDataJSON: new Uint8Array([1]).buffer, attestationObject: new Uint8Array([2]).buffer },
      };
    });
    vi.stubGlobal("navigator", { ...navigator, credentials: { create } });
    await render(<PasskeySettings />);
    expect(button("Add a passkey")!.disabled).toBe(true); // no password yet
    await act(async () => {
      type(host.querySelector('input[aria-label="Name for the new passkey"]') as HTMLInputElement, "Laptop");
      type(host.querySelector('input[aria-label="Current password"]') as HTMLInputElement, "a long password");
    });
    await act(async () => button("Add a passkey")!.click());
    await settle();
    expect(sent.find((s) => s.url === "/api/profile/passkeys/options")?.body).toEqual({
      current_password: "a long password",
    });
    expect(sent.find((s) => s.url === "/api/profile/passkeys" && s.method === "POST")?.body).toEqual({
      challenge_id: "r1", name: "Laptop", client_data_json: "AQ", attestation_object: "Ag",
    });
  });
});
