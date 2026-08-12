import type { ApiError } from "./types";

// The API is same-origin: in production the client is served by lancastd; in
// development Vite proxies /api to it. So relative paths are all that is needed.

export class ApiFailure extends Error {
  code: string;
  status: number;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "ApiFailure";
    this.status = status;
    this.code = code;
  }
}

export async function apiGet<T>(path: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(path, { signal });
  if (res.status === 204) return null as T;
  const body = await res.json().catch(() => null);
  if (!res.ok) {
    const err = body as ApiError | null;
    throw new ApiFailure(
      res.status,
      err?.error?.code ?? "error",
      err?.error?.message ?? res.statusText,
    );
  }
  return body as T;
}

// apiSend performs a write (PUT/POST/PATCH/DELETE), sending JSON when a body is
// given and tolerating the empty 204 responses these endpoints return.
export async function apiSend(
  path: string,
  method: "PUT" | "POST" | "PATCH" | "DELETE",
  body?: unknown,
): Promise<void> {
  const res = await fetch(path, {
    method,
    headers: body !== undefined ? { "Content-Type": "application/json" } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (!res.ok && res.status !== 204) {
    const err = (await res.json().catch(() => null)) as ApiError | null;
    throw new ApiFailure(
      res.status,
      err?.error?.code ?? "error",
      err?.error?.message ?? res.statusText,
    );
  }
}

// apiPost sends JSON and returns the parsed response body — for writes that
// answer with data (a created resource), unlike apiSend which discards it.
export async function apiPost<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  const parsed = await res.json().catch(() => null);
  if (!res.ok) {
    const err = parsed as ApiError | null;
    throw new ApiFailure(
      res.status,
      err?.error?.code ?? "error",
      err?.error?.message ?? res.statusText,
    );
  }
  return parsed as T;
}

// apiUpload sends raw bytes (a plugin bundle) and returns the parsed response.
// Distinct from apiPost, which sends JSON.
export async function apiUpload<T>(path: string, data: ArrayBuffer): Promise<T> {
  const res = await fetch(path, {
    method: "POST",
    headers: { "Content-Type": "application/octet-stream" },
    body: data,
  });
  const parsed = await res.json().catch(() => null);
  if (!res.ok) {
    const err = parsed as ApiError | null;
    throw new ApiFailure(
      res.status,
      err?.error?.code ?? "error",
      err?.error?.message ?? res.statusText,
    );
  }
  return parsed as T;
}

// artworkURL builds a content-addressed image URL. The bytes behind a hash
// never change, so these are cached immutably by the server.
export function artworkURL(
  hash: string | undefined,
  size: "thumb" | "poster" | "poster2x" | "fanart" | "original",
): string | undefined {
  if (!hash) return undefined;
  return `/api/artwork/${hash}?size=${size}`;
}
