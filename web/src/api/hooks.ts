import { useQuery, keepPreviousData } from "@tanstack/react-query";
import { apiGet } from "./client";
import type { ItemsPage, Library } from "./types";

export function useLibraries() {
  return useQuery({
    queryKey: ["libraries"],
    queryFn: ({ signal }) => apiGet<Library[]>("/api/libraries", signal),
    staleTime: 30_000,
  });
}

export interface ItemQuery {
  libraryID: number;
  q?: string;
  limit?: number;
  offset?: number;
}

export function useItems({ libraryID, q, limit = 120, offset = 0 }: ItemQuery) {
  const params = new URLSearchParams({
    library_id: String(libraryID),
    limit: String(limit),
    offset: String(offset),
  });
  if (q) params.set("q", q);

  return useQuery({
    queryKey: ["items", libraryID, q ?? "", limit, offset],
    queryFn: ({ signal }) =>
      apiGet<ItemsPage>(`/api/items?${params.toString()}`, signal),
    // Keep the previous grid visible while a new search or page loads, so the
    // library does not flash empty on every keystroke.
    placeholderData: keepPreviousData,
    enabled: libraryID > 0,
  });
}
