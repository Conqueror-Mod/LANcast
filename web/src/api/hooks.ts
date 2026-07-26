import {
  useQuery,
  useMutation,
  useQueryClient,
  keepPreviousData,
} from "@tanstack/react-query";
import { apiGet, apiPost, apiSend } from "./client";
import type {
  Item,
  ItemsPage,
  Library,
  MatchCandidate,
  ScanStatus,
  Settings,
  SettingsUpdate,
  SubtitleCandidate,
  SubtitleSearchResult,
  SubtitleTrack,
  Trailer,
} from "./types";

export function useSettings() {
  return useQuery({
    queryKey: ["settings"],
    queryFn: ({ signal }) => apiGet<Settings>("/api/settings", signal),
  });
}

export function useUpdateSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (update: SettingsUpdate) =>
      apiSend("/api/settings", "PUT", update),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["settings"] }),
  });
}

export function useCreateLibrary() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (lib: { name: string; kind: string; path: string }) =>
      apiSend("/api/libraries", "POST", lib),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["libraries"] }),
  });
}

// A scan (and a metadata refresh) run in the background; the caller then polls
// useScanStatus to show progress.
export function useStartScan() {
  return useMutation({
    mutationFn: (libraryID: number) =>
      apiSend(`/api/libraries/${libraryID}/scan`, "POST"),
  });
}

export function useRefreshLibrary() {
  return useMutation({
    mutationFn: (libraryID: number) =>
      apiSend(`/api/libraries/${libraryID}/refresh`, "POST"),
  });
}

// Polls scan status while a scan is running; idle once it finishes, at which
// point the library list is refreshed so counts and "last scanned" update.
export function useScanStatus(libraryID: number) {
  const qc = useQueryClient();
  return useQuery({
    queryKey: ["scan", libraryID],
    queryFn: async ({ signal }) => {
      const s = await apiGet<ScanStatus>(
        `/api/libraries/${libraryID}/scan`,
        signal,
      );
      if (s.state !== "running") {
        qc.invalidateQueries({ queryKey: ["libraries"] });
      }
      return s;
    },
    refetchInterval: (q) =>
      q.state.data?.state === "running" ? 1000 : false,
    enabled: libraryID > 0,
  });
}

export function useLibraries() {
  return useQuery({
    queryKey: ["libraries"],
    queryFn: ({ signal }) => apiGet<Library[]>("/api/libraries", signal),
    staleTime: 30_000,
  });
}

export function useItem(id: number) {
  return useQuery({
    queryKey: ["item", id],
    queryFn: ({ signal }) => apiGet<Item>(`/api/items/${id}`, signal),
    enabled: id > 0,
  });
}

// The trailer is a separate call: it is optional, and a detail page should
// render fully without waiting on it. A 404 is a normal "no trailer" answer.
// Provider matches for correcting an item's identity. Lazy: only searches once
// a query is set, so opening Fix match does not fire a provider call until asked.
export function useCandidates(id: number, query: string | null) {
  return useQuery({
    queryKey: ["candidates", id, query],
    queryFn: ({ signal }) => {
      const p = query ? `?q=${encodeURIComponent(query)}` : "";
      return apiGet<MatchCandidate[]>(`/api/items/${id}/candidates${p}`, signal);
    },
    enabled: id > 0 && query !== null,
    staleTime: 60_000,
    retry: false,
  });
}

// Confirming a match locks the identity against re-scoring and requeues the
// item for enrichment. The item (and any list showing it) is refreshed.
export function useApplyMatch(id: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (c: MatchCandidate) =>
      apiSend(`/api/items/${id}/match`, "POST", {
        provider: c.Provider,
        external_id: c.ExternalID,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["item", id] });
      qc.invalidateQueries({ queryKey: ["items"] });
      qc.invalidateQueries({ queryKey: ["recently-added"] });
    },
  });
}

// Releasing a locked field lets a future refresh or match overwrite it again.
export function useUnlockField(id: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (field: string) =>
      apiSend(`/api/items/${id}/locks/${encodeURIComponent(field)}`, "DELETE"),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["item", id] }),
  });
}

export function useTrailer(id: number) {
  return useQuery({
    queryKey: ["trailer", id],
    queryFn: ({ signal }) =>
      apiGet<{ trailer: Trailer | null }>(`/api/items/${id}/trailer`, signal)
        .then((r) => r.trailer)
        .catch(() => null),
    enabled: id > 0,
    staleTime: Infinity,
  });
}

export function useContinueWatching(limit = 20) {
  return useQuery({
    queryKey: ["continue", limit],
    queryFn: ({ signal }) =>
      apiGet<{ items: Item[] }>(`/api/continue?limit=${limit}`, signal).then(
        (r) => r.items,
      ),
    staleTime: 10_000,
  });
}

// Recently added, across every library. Its own hook rather than useItems so a
// home shelf is not entangled with the Browse grid's library scoping.
export function useRecentlyAdded(limit = 20) {
  return useQuery({
    queryKey: ["recently-added", limit],
    queryFn: ({ signal }) =>
      apiGet<ItemsPage>(`/api/items?sort=added&limit=${limit}`, signal).then(
        (r) => r.items,
      ),
    staleTime: 30_000,
  });
}

export function useSubtitles(id: number) {
  return useQuery({
    queryKey: ["subtitles", id],
    queryFn: ({ signal }) =>
      apiGet<{ tracks: SubtitleTrack[] }>(
        `/api/items/${id}/subtitles`,
        signal,
      ).then((r) => r.tracks),
    enabled: id > 0,
  });
}

// Online subtitle search. Enabled only once a query is set, so opening the
// picker does not fire a provider request until the user asks for one.
export function useSubtitleSearch(
  id: number,
  query: string | null,
  language: string,
) {
  return useQuery({
    queryKey: ["subtitle-search", id, query, language],
    queryFn: ({ signal }) => {
      const p = new URLSearchParams({ language });
      if (query) p.set("q", query);
      return apiGet<SubtitleSearchResult>(
        `/api/items/${id}/subtitles/search?${p.toString()}`,
        signal,
      );
    },
    enabled: id > 0 && query !== null,
    staleTime: 60_000,
    retry: false,
  });
}

// Downloading a subtitle consumes provider quota, so it is an explicit action.
// It returns the new track's key; on success the item's track list is
// refreshed so the track appears in the picker.
export function useDownloadSubtitle(id: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (c: SubtitleCandidate) =>
      apiPost<{ key: string; language: string; label: string }>(
        `/api/items/${id}/subtitles/download`,
        {
          file_id: c.file_id,
          language: c.language,
          file_name: c.file_name || c.release,
        },
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["subtitles", id] }),
  });
}

export function useDeleteSubtitle(id: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (key: string) =>
      apiSend(`/api/items/${id}/subtitles/${encodeURIComponent(key)}`, "DELETE"),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["subtitles", id] }),
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
