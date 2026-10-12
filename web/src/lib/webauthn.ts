/*
 * Passkey ceremonies against the server (ADR 0082).
 *
 * The server speaks base64url for every binary value; navigator.credentials
 * speaks ArrayBuffer. This file is the only place that converts between them,
 * so the routes and the browser API can each stay in their own shape.
 */
import { apiPost } from "@/api/client";
import type { AuthUser } from "@/api/types";

export function fromB64url(s: string): ArrayBuffer {
  const b64 = s.replace(/-/g, "+").replace(/_/g, "/");
  const padded = b64 + "=".repeat((4 - (b64.length % 4)) % 4);
  const bin = atob(padded);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out.buffer;
}

export function toB64url(buf: ArrayBuffer | ArrayBufferView | null): string {
  if (!buf) return "";
  // isView rather than instanceof ArrayBuffer: a buffer can come from another
  // realm (a frame, a test environment), where instanceof is false and the
  // value would be read as empty.
  const bytes = ArrayBuffer.isView(buf)
    ? new Uint8Array(buf.buffer, buf.byteOffset, buf.byteLength)
    : new Uint8Array(buf);
  let bin = "";
  for (const b of bytes) bin += String.fromCharCode(b);
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

interface Options {
  challenge_id: string;
  public_key: Record<string, unknown>;
}

/*
 * A refusal from the browser, as opposed to one from the server.
 *
 * NotAllowedError is what every browser throws when the person closes the
 * Windows Hello or phone prompt, and also when the prompt times out. Neither
 * is a failure worth a red error, so callers can tell it apart and say
 * something quieter.
 */
export function cancelledByPerson(err: unknown): boolean {
  return err instanceof DOMException && err.name === "NotAllowedError";
}

export async function signInWithPasskey(): Promise<{ authenticated: boolean; user: AuthUser }> {
  const opts = await apiPost<Options>("/api/auth/passkey/login/options", {});
  const pk = opts.public_key;
  const cred = (await navigator.credentials.get({
    publicKey: {
      ...(pk as unknown as PublicKeyCredentialRequestOptions),
      challenge: fromB64url(pk.challenge as string),
    },
  })) as PublicKeyCredential | null;
  if (!cred) throw new DOMException("No passkey chosen", "NotAllowedError");
  const r = cred.response as AuthenticatorAssertionResponse;
  return apiPost("/api/auth/passkey/login", {
    challenge_id: opts.challenge_id,
    id: toB64url(cred.rawId),
    client_data_json: toB64url(r.clientDataJSON),
    authenticator_data: toB64url(r.authenticatorData),
    signature: toB64url(r.signature),
    user_handle: toB64url(r.userHandle),
  });
}

export async function addPasskey(currentPassword: string, name: string): Promise<unknown> {
  const opts = await apiPost<Options>("/api/profile/passkeys/options", {
    current_password: currentPassword,
  });
  const pk = opts.public_key as {
    challenge: string;
    user: { id: string; name: string; displayName: string };
    excludeCredentials?: { type: "public-key"; id: string }[];
  } & Record<string, unknown>;
  const cred = (await navigator.credentials.create({
    publicKey: {
      ...(pk as unknown as PublicKeyCredentialCreationOptions),
      challenge: fromB64url(pk.challenge),
      user: { ...pk.user, id: fromB64url(pk.user.id) },
      excludeCredentials: (pk.excludeCredentials ?? []).map((c) => ({
        type: c.type,
        id: fromB64url(c.id),
      })),
    },
  })) as PublicKeyCredential | null;
  if (!cred) throw new DOMException("No passkey made", "NotAllowedError");
  const r = cred.response as AuthenticatorAttestationResponse;
  return apiPost("/api/profile/passkeys", {
    challenge_id: opts.challenge_id,
    name,
    client_data_json: toB64url(r.clientDataJSON),
    attestation_object: toB64url(r.attestationObject),
  });
}
