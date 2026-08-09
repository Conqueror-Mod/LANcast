import {
  useQuery,
  useInfiniteQuery,
  useMutation,
  useQueryClient,
  keepPreviousData,
} from "@tanstack/react-query";
import { apiGet, apiPost, apiSend, apiUpload } from "./client";
import type {
  ActivityStatus,
  AuditPage,
  AuthStatus,
  AuthUser,
  BrowseResult,
  Facets,
  Item,
  ItemsPage,
  Library,
  MatchCandidate,
  Plugin,
  PluginCaps,
  ProbeStatus,
  ReprobeResult,
  ScanStatus,
  ServerLog,
  UpdateStatus,
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

// Background probing progress. Polled only while a pass is running: probing is
// a background chore, and a settings page that polls forever is a settings page
// that never lets the machine idle.
export function useProbeStatus(enabled = true) {
  return useQuery({
    queryKey: ["probe"],
    queryFn: () => apiGet<ProbeStatus>("/api/probe"),
    enabled,
    refetchInterval: (q) => (q.state.data?.running ? 2000 : false),
  });
}

// Re-probes already-probed files. "incomplete" re-reads only what a current
// build would learn something from; "all" re-reads everything, which on a large
// library is hours of ffprobe.
export function useReprobe() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (scope: "incomplete" | "all") =>
      apiPost<ReprobeResult>(`/api/probe/refresh?scope=${scope}`, {}),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["probe"] }),
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
// `sort` is passed through when the default order is not the one the container
// plays in. An album needs "track": the default leads with title, and a track —
// unlike an episode, which inherits its series' sort title and therefore ties —
// keeps its own, so an album asked for without it arrives alphabetically. It is
// part of the query key, or two callers wanting different orders would share one
// cached answer.
export function useChildren(parentID: number, enabled: boolean, sort?: string) {
  return useQuery({
    queryKey: ["children", parentID, sort ?? ""],
    queryFn: ({ signal }) =>
      apiGet<ItemsPage>(
        `/api/items?parent_id=${parentID}` + (sort ? `&sort=${sort}` : ""),
        signal,
      ).then((r) => r.items),
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

// Removes a title. mode "ignore" keeps the files on disk (and skips them on
// future scans); mode "delete" removes the files too. Every list that could be
// showing the item is refreshed afterwards.
export function useDeleteItem(id: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (mode: "ignore" | "delete") =>
      apiSend(`/api/items/${id}?mode=${mode}`, "DELETE"),
    onSuccess: () => {
      for (const key of [
        "items",
        "recently-added",
        "continue",
        "review",
        "children",
        "collection-members",
      ]) {
        qc.invalidateQueries({ queryKey: [key] });
      }
    },
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
        // The candidate's kind may differ from the item's — correcting a
        // movie-scanned miniseries to its TV entry — so the server fetches from
        // the right endpoint.
        kind: c.Kind,
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

// `enabled` lets the caller skip the request where it cannot have an answer —
// a music track, whose subtitle list is always empty.
export function useSubtitles(id: number, enabled = true) {
  return useQuery({
    queryKey: ["subtitles", id],
    queryFn: ({ signal }) =>
      apiGet<{ tracks: SubtitleTrack[] }>(
        `/api/items/${id}/subtitles`,
        signal,
      ).then((r) => r.tracks),
    enabled: enabled && id > 0,
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
  sort?: string; // title | year | added
  genres?: string[];
  decades?: number[];
  contentRatings?: string[];
  unwatched?: boolean;
  limit?: number;
  offset?: number;
}

// itemsParams builds the grid query string. Shared by the single-page and
// paging hooks so the two can never disagree about how a filter is encoded.
function itemsParams({
  libraryID,
  q,
  sort,
  genres = [],
  decades = [],
  contentRatings = [],
  unwatched = false,
  limit = 120,
  offset = 0,
}: ItemQuery): URLSearchParams {
  const params = new URLSearchParams({
    library_id: String(libraryID),
    limit: String(limit),
    offset: String(offset),
  });
  if (q) params.set("q", q);
  if (sort) params.set("sort", sort);
  // Repeatable facet filters — one param per chosen value (OR within a facet).
  for (const g of genres) params.append("genre", g);
  for (const d of decades) params.append("decade", String(d));
  for (const c of contentRatings) params.append("content_rating", c);
  if (unwatched) params.set("watched", "false");
  return params;
}

export function useItems(query: ItemQuery) {
  const { libraryID } = query;
  const params = itemsParams(query);

  return useQuery({
    // The query string fully identifies the request, so it is the cache key —
    // no need to enumerate every filter dimension by hand.
    queryKey: ["items", params.toString()],
    queryFn: ({ signal }) =>
      apiGet<ItemsPage>(`/api/items?${params.toString()}`, signal),
    // Keep the previous grid visible while a new search or page loads, so the
    // library does not flash empty on every keystroke.
    placeholderData: keepPreviousData,
    enabled: libraryID > 0,
  });
}

// The filter values a library's browse view offers — genres, decades, and
// content ratings actually present, plus whether an unwatched toggle is worth
// showing, so a chosen filter never empties the grid.
export function useFacets(libraryID: number) {
  return useQuery({
    queryKey: ["facets", libraryID],
    queryFn: ({ signal }) =>
      apiGet<Facets>(`/api/libraries/${libraryID}/facets`, signal),
    enabled: libraryID > 0,
    staleTime: 30_000,
  });
}

// ------------------------------------------------------------------ plugins (admin)

// Installed plugins with their signer, enabled state, and requested vs granted
// capabilities (ADR 0021). Admin-only; callers pass their admin status as
// `enabled` so a member never fires the 403.
export function usePlugins(enabled = true) {
  return useQuery({
    queryKey: ["plugins"],
    queryFn: ({ signal }) =>
      apiGet<{ plugins: Plugin[] }>("/api/plugins", signal).then((r) => r.plugins),
    enabled,
    staleTime: 15_000,
  });
}

// Step one of install: upload a .lcplugin, which is verified and staged disabled.
// Returns what it requests so the UI can present the capability-approval dialog.
export function useUploadPlugin() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: ArrayBuffer) => apiUpload<Plugin>("/api/plugins", data),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["plugins"] }),
  });
}

// Step two: approve a subset of the requested capabilities and activate.
export function useGrantPlugin() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { name: string; caps: PluginCaps }) =>
      apiPost<Plugin>(`/api/plugins/${encodeURIComponent(args.name)}/grant`, args.caps),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["plugins"] }),
  });
}

export function useSetPluginEnabled() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { name: string; enabled: boolean }) =>
      apiSend(
        `/api/plugins/${encodeURIComponent(args.name)}/${args.enabled ? "enable" : "disable"}`,
        "POST",
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["plugins"] }),
  });
}

export function useRemovePlugin() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (name: string) =>
      apiSend(`/api/plugins/${encodeURIComponent(name)}`, "DELETE"),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["plugins"] }),
  });
}

// The browse grid pages through the library rather than fetching it all: a real
// library is thousands of items, and a single capped request silently truncated
// it — the grid stopped dead partway through the alphabet with no indication
// there was more. Pages are appended as the user scrolls.
const BROWSE_PAGE_SIZE = 120;

export function useInfiniteItems(query: Omit<ItemQuery, "limit" | "offset">) {
  const base = itemsParams({ ...query, limit: BROWSE_PAGE_SIZE, offset: 0 });
  return useInfiniteQuery({
    queryKey: ["items-infinite", base.toString()],
    initialPageParam: 0,
    queryFn: ({ pageParam, signal }) => {
      const params = itemsParams({
        ...query,
        limit: BROWSE_PAGE_SIZE,
        offset: pageParam as number,
      });
      return apiGet<ItemsPage>(`/api/items?${params.toString()}`, signal);
    },
    // Stop when the pages so far account for everything the server reported.
    getNextPageParam: (last, pages) => {
      const loaded = pages.reduce((n, p) => n + p.items.length, 0);
      return loaded < last.total ? loaded : undefined;
    },
    enabled: (query.libraryID ?? 0) > 0,
  });
}

// ------------------------------------------------------------------ activity

// What the server is doing right now, in one request. The pieces already
// existed per-worker; this is the one caller that does not need to know which
// worker to ask, which is what lets the shell show a single indicator.
//
// It polls whenever the app is visible, faster while something is running. That
// is a deliberate exception to the "do not poll a settings page forever" rule:
// an indicator that only updates when you open it is not an indicator. React
// Query stops the interval when the tab is hidden, so an idle machine idles.
export function useActivity() {
  const qc = useQueryClient();
  return useQuery({
    queryKey: ["activity"],
    queryFn: async ({ signal }) => {
      const s = await apiGet<ActivityStatus>("/api/activity", signal);
      // Work finishing changes item counts, so the nav is refreshed once here
      // rather than every poll.
      if (!s.active && qc.getQueryData<ActivityStatus>(["activity"])?.active) {
        qc.invalidateQueries({ queryKey: ["libraries"] });
      }
      return s;
    },
    refetchInterval: (q) => (q.state.data?.active ? 1500 : 8000),
  });
}

// The tail of lancastd.log. Not polled: a log is what already happened, and the
// button that opens it is the refresh. Enabled only when the viewer opens the
// panel, so an admin who never looks never reads a file off disk.
export function useServerLog(enabled: boolean, lines = 300) {
  return useQuery({
    queryKey: ["logs", lines],
    queryFn: ({ signal }) =>
      apiGet<ServerLog>(`/api/logs?lines=${lines}`, signal),
    enabled,
    staleTime: 0,
    gcTime: 0,
  });
}

// ------------------------------------------------------------------ audit

// The audit log (ADR 0026). Not polled: it records deliberate acts, which do
// not happen while you are staring at the page. Opening the panel and the
// Refresh button are the two things that should fetch it.
//
// Paging is by offset with a growing limit rather than an infinite query,
// because the useful reading of an audit log is "the most recent N", and N
// grows only when someone asks for more.
export function useAuditLog(enabled: boolean, action: string, limit: number) {
  return useQuery({
    queryKey: ["audit", action, limit],
    queryFn: ({ signal }) => {
      const q = new URLSearchParams({ limit: String(limit) });
      if (action) q.set("action", action);
      return apiGet<AuditPage>(`/api/audit?${q}`, signal);
    },
    enabled,
    staleTime: 0,
    gcTime: 0,
  });
}

// ------------------------------------------------------------------ updates

// The update status. Cheap and static — the server checks on its own timer, so
// this only reads what it already knows.
export function useUpdateStatus() {
  return useQuery({
    queryKey: ["update"],
    queryFn: ({ signal }) => apiGet<UpdateStatus>("/api/update", signal),
    staleTime: 60_000,
  });
}

// Asks now. Works whether or not the automatic check is enabled: someone who
// does not want a timer may still want to ask once, deliberately.
export function useCheckForUpdate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => apiPost<UpdateStatus>("/api/update/check", {}),
    onSuccess: (s) => {
      qc.setQueryData(["update"], s);
      qc.invalidateQueries({ queryKey: ["activity"] });
    },
  });
}

// Downloads and stages an update. Returns immediately — the work runs on the
// server and its progress shows in the activity panel, which is why this polls
// the status for a while afterwards rather than waiting.
export function useDownloadUpdate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => apiSend("/api/update/download", "POST"),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["activity"] });
      qc.invalidateQueries({ queryKey: ["update"] });
    },
  });
}
