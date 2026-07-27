import {
  useQuery,
  useMutation,
  useQueryClient,
  keepPreviousData,
} from "@tanstack/react-query";
import { apiGet, apiPost, apiSend } from "./client";
import type {
  AuthStatus,
  AuthUser,
  BrowseResult,
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

// ------------------------------------------------------------------ auth

// The auth gate reads from here on every render, so it is cached and only
// refetched when a sign-in, sign-out, or 401 invalidates it.
export function useAuthStatus() {
  return useQuery({
    queryKey: ["auth-status"],
    queryFn: ({ signal }) => apiGet<AuthStatus>("/api/auth/status", signal),
    staleTime: 30_000,
  });
}

// The signed-in user, or undefined. A thin read over the cached status so any
// component can role-gate a control without wiring its own query.
export function useCurrentUser(): AuthUser | undefined {
  return useAuthStatus().data?.user;
}

export function useIsAdmin(): boolean {
  return useCurrentUser()?.role === "admin";
}

export function useSetup() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (creds: { username: string; password: string }) =>
      apiPost<AuthStatus>("/api/auth/setup", creds),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["auth-status"] }),
  });
}

export function useLogin() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (creds: { username: string; password: string }) =>
      apiPost<{ authenticated: boolean; user: AuthUser }>(
        "/api/auth/login",
        creds,
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["auth-status"] }),
  });
}

// Logout drops every cached query except auth status, so no per-user data
// (watch progress, admin-only lists) bleeds into the next session — then
// refetches auth status so the App gate flips to the login screen. Clearing the
// auth-status query instead of refetching it would leave the gate with no data
// to re-evaluate, which is the bug this replaced.
export function useLogout() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => apiSend("/api/auth/logout", "POST"),
    onSuccess: () => {
      qc.removeQueries({
        predicate: (q) => q.queryKey[0] !== "auth-status",
      });
      qc.invalidateQueries({ queryKey: ["auth-status"] });
    },
  });
}

// Changes the caller's own password. The server revokes their sessions, so the
// UI returns to the login screen afterwards.
export function useChangePassword() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { current_password: string; new_password: string }) =>
      apiSend("/api/auth/password", "POST", body),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["auth-status"] }),
  });
}

// ------------------------------------------------------------------ users (admin)

export function useUsers() {
  return useQuery({
    queryKey: ["users"],
    queryFn: ({ signal }) =>
      apiGet<{ users: AuthUser[] }>("/api/users", signal).then((r) => r.users),
  });
}

export function useCreateUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (u: { username: string; password: string; role: string }) =>
      apiPost<AuthUser>("/api/users", u),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });
}

export function useDeleteUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => apiSend(`/api/users/${id}`, "DELETE"),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });
}

export function useResetUserPassword() {
  return useMutation({
    mutationFn: (args: { id: string; password: string }) =>
      apiSend(`/api/users/${args.id}/password`, "POST", {
        new_password: args.password,
      }),
  });
}

// Lists directories for the folder picker. An empty path asks for the roots
// (drive letters on Windows, / elsewhere).
export function useBrowse(path: string) {
  return useQuery({
    queryKey: ["browse", path],
    queryFn: ({ signal }) => {
      const p = path ? `?path=${encodeURIComponent(path)}` : "";
      return apiGet<BrowseResult>(`/api/browse${p}`, signal);
    },
    staleTime: 15_000,
  });
}

// Settings is an admin-only endpoint, so members must not fire it — a 403 on
// mount is noise. Callers pass their admin status as `enabled`.
export function useSettings(enabled = true) {
  return useQuery({
    queryKey: ["settings"],
    queryFn: ({ signal }) => apiGet<Settings>("/api/settings", signal),
    enabled,
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

// Forgets a library (never deletes files). Its items vanish from every view, so
// the lists that could be showing them are refreshed.
export function useDeleteLibrary() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (libraryID: number) =>
      apiSend(`/api/libraries/${libraryID}`, "DELETE"),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["libraries"] });
      qc.invalidateQueries({ queryKey: ["items"] });
      qc.invalidateQueries({ queryKey: ["review"] });
      qc.invalidateQueries({ queryKey: ["recently-added"] });
      qc.invalidateQueries({ queryKey: ["continue"] });
    },
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

// The parent_id children of a container — a show's seasons, a season's
// episodes, a work's parts — in hierarchy order. Enabled only for containers, so
// a plain leaf detail never fires it. A collection is the exception: its members
// live in a join table, so it uses useCollectionMembers instead.
export function useChildren(parentID: number, enabled: boolean) {
  return useQuery({
    queryKey: ["children", parentID],
    queryFn: ({ signal }) =>
      apiGet<ItemsPage>(`/api/items?parent_id=${parentID}`, signal).then(
        (r) => r.items,
      ),
    enabled: enabled && parentID > 0,
  });
}

// The members of a collection, in curated order. Membership is many-to-many and
// keyed through item_collection, so it is a different endpoint from the
// parent_id children above.
export function useCollectionMembers(collectionID: number, enabled: boolean) {
  return useQuery({
    queryKey: ["collection-members", collectionID],
    queryFn: ({ signal }) =>
      apiGet<ItemsPage>(`/api/items?collection_id=${collectionID}`, signal).then(
        (r) => r.items,
      ),
    enabled: enabled && collectionID > 0,
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
      qc.invalidateQueries({ queryKey: ["review"] });
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

// Items whose identity is uncertain — review (applied but flagged) and
// unmatched (nothing good enough was found). The metadata-health queue.
export function useReview(libraryID?: number) {
  const p = libraryID ? `?library_id=${libraryID}` : "";
  return useQuery({
    queryKey: ["review", libraryID ?? 0],
    queryFn: ({ signal }) =>
      apiGet<{ items: Item[]; total: number }>(`/api/review${p}`, signal),
    staleTime: 15_000,
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
