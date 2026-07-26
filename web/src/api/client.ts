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

// artworkURL builds a content-addressed image URL. The bytes behind a hash
// never change, so these are cached immutably by the server.
export function artworkURL(
  hash: string | undefined,
  size: "thumb" | "poster" | "poster2x" | "fanart" | "original",
): string | undefined {
  if (!hash) return undefined;
  return `/api/artwork/${hash}?size=${size}`;
}
