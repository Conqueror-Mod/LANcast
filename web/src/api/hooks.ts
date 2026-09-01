import {
  useQuery,
  useInfiniteQuery,
  useMutation,
  useQueryClient,
  keepPreviousData,
  type QueryClient,
} from "@tanstack/react-query";
import { apiGet, apiPost, apiSend, apiUpload } from "./client";
import { isContainer } from "@/lib/kind";
import { forgetAcknowledgements } from "@/lib/sensitiveAck";
import type {
  ActivityStatus,
  AuditPage,
  AuthStatus,
  AuthUser,
  BrowseResult,
  Channel,
  ChannelSource,
  Program,
  GuideNow,
  Person,
  CastMember,
  Collision,
  CrashReport,
  MediaToolsState,
  Facets,
  HistoryEntry,
  Item,
  ItemsPage,
  Library,
  MatchCandidate,
  Plugin,
  PluginCaps,
  Profile,
  RatedItem,
  Rating,
  Role,
  ProbeStatus,
  ReprobeResult,
  ReparseResult,
  ScanAllResult,
  ScanStatus,
  ServerLog,
  UpdateStatus,
  Settings,
  SettingsUpdate,
  SubtitleCandidate,
  SubtitleSearchResult,
  SubtitleTrack,
  Trailer,
  Trending,
  PeerPresence,
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
    mutationFn: (creds: {
      username: string;
      password: string;
      /*
       * Sent only when the person actually saw the option (ADR 0048).
       *
       * Optional rather than defaulted here: absent means "was never asked"
       * and the server treats that as no. A client that quietly sent `true`
       * would be manufacturing the consent the disclosure exists to obtain.
       */
      install_media_tools?: boolean;
    }) => apiPost<AuthStatus>("/api/auth/setup", creds),
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
      /*
       * And whatever this person agreed to look at (ADR 0051).
       *
       * The acknowledgement is theirs. A session store that survives one person
       * signing out and another signing in is a store that shows the second
       * person what the first agreed to look at, which is the failure the whole
       * feature exists to prevent — arriving at the exact moment somebody else
       * is at the keyboard.
       */
      forgetAcknowledgements();
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
/*
 * Marking an item sensitive, or clearing the mark (ADR 0051).
 *
 * The mark changes what a *thumbnail* looks like, and thumbnails are drawn from
 * every list there is — the grid, the shelves on the home page, search results,
 * a collection. So this invalidates the lists rather than the item, which is
 * the opposite of the instinct and the reason this project's most-repeated bug
 * keeps happening: the write succeeds, the server is right, and the picture
 * somebody is looking at is the stale one.
 *
 * `children` is invalidated too, because the mark travels down: marking a
 * folder covers every photograph inside it, and those are what the folder page
 * is listing.
 */
export function useSetSensitive() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, sensitive }: { id: number; sensitive: boolean }) =>
      apiSend(`/api/items/${id}/sensitive`, "PUT", { sensitive }),
    onSuccess: (_data, { id }) => {
      qc.invalidateQueries({ queryKey: ["item", id] });
      qc.invalidateQueries({ queryKey: ["items"] });
      qc.invalidateQueries({ queryKey: ["items-infinite"] });
      qc.invalidateQueries({ queryKey: ["children"] });
      qc.invalidateQueries({ queryKey: ["recently-added"] });
      qc.invalidateQueries({ queryKey: ["search"] });
    },
  });
}

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
/**
 * Edit a library: its name, its path, or both.
 *
 * Not its kind — the server refuses that, and the client does not offer it. A
 * kind decides which scanner runs and what the top level of the browse is;
 * changing it would leave a library describing itself as something its rows are
 * not.
 */
export function useUpdateLibrary() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: number; name?: string; path?: string }) =>
      apiSend(`/api/libraries/${v.id}`, "PATCH", {
        name: v.name,
        path: v.path,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["libraries"] });
      // A repoint rewrites every item path in the library, so anything holding
      // items has to be re-read rather than trusted.
      qc.invalidateQueries({ queryKey: ["items"] });
      qc.invalidateQueries({ queryKey: ["item"] });
    },
  });
}

/*
 * A library's locations (ADR 0034).
 *
 * All three invalidate `items` as well as `libraries`, and for different
 * reasons that all end the same way: adding one changes what a scan will find,
 * removing one deletes every row under it, and moving one rewrites every item
 * path in it. Anything holding items has to be re-read rather than trusted.
 */
export function useAddRoot() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { libraryID: number; path: string }) =>
      apiSend(`/api/libraries/${v.libraryID}/roots`, "POST", { path: v.path }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["libraries"] });
    },
  });
}

/**
 * Remove a location and every item scanned under it.
 *
 * This deletes rows where an unreachable drive only marks them missing, which
 * is why the caller has to say the count out loud before asking.
 */
export function useRemoveRoot() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { libraryID: number; rootID: number }) =>
      apiSend(`/api/libraries/${v.libraryID}/roots/${v.rootID}`, "DELETE"),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["libraries"] });
      qc.invalidateQueries({ queryKey: ["items"] });
      qc.invalidateQueries({ queryKey: ["item"] });
      qc.invalidateQueries({ queryKey: ["review"] });
      qc.invalidateQueries({ queryKey: ["recently-added"] });
      qc.invalidateQueries({ queryKey: ["continue"] });
    },
  });
}

/** Move one location, carrying its contents with it — the drive-letter case. */
export function useRepointRoot() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { libraryID: number; rootID: number; path: string }) =>
      apiSend(`/api/libraries/${v.libraryID}/roots/${v.rootID}`, "PATCH", {
        path: v.path,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["libraries"] });
      qc.invalidateQueries({ queryKey: ["items"] });
      qc.invalidateQueries({ queryKey: ["item"] });
    },
  });
}

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

/*
 * Starting background work has to *say* it started.
 *
 * A scan and a metadata refresh both return 202 and run in the background, and
 * both used to return without touching the cache. Nothing then knew anything
 * had changed until the activity poll came round on its own, which when idle is
 * every 8 seconds — so pressing Scan produced no visible reaction at all, and
 * pressing it again looked like the first press had been missed.
 *
 * Worse, a small library finishes inside that window. The activity poll only
 * refreshes the nav's item counts when it *sees* work go from active to idle,
 * so a scan that began and ended between two polls was never observed running,
 * the transition never happened, and the sidebar kept a count from before the
 * scan — indefinitely, since nothing else invalidates it.
 *
 * So the mutation marks activity active itself. It is not a guess: Scanner.Start
 * sets the state to running before the 202 is written, so by the time this runs
 * the work really is in progress. Claiming it here means the next poll sees a
 * true active-to-idle edge whether the scan takes eight seconds or one, and the
 * counts refresh either way.
 */
function useBackgroundLibraryJob(path: (libraryID: number) => string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (libraryID: number) => apiSend(path(libraryID), "POST"),
    onSuccess: (_res, libraryID) => {
      qc.setQueryData<ActivityStatus>(["activity"], (prev) => ({
        active: true,
        tasks: prev?.tasks ?? [],
      }));
      // Refetch both now rather than waiting out the idle interval: the panel
      // starts showing progress, and the per-library status query starts its
      // own faster poll.
      qc.invalidateQueries({ queryKey: ["activity"] });
      qc.invalidateQueries({ queryKey: ["scan", libraryID] });
    },
  });
}

/*
 * What a finished scan changes.
 *
 * Both poll loops below used to refresh ["libraries"] alone, on the reasoning
 * that a scan changes counts and the "last scanned" stamp. It changes rather
 * more than that: a scan is the thing that marks a deleted file missing, and
 * every list here filters missing rows out on the server. Refreshing only the
 * counts meant the sidebar learned the film was gone while the grid beside it
 * went on showing the poster — indefinitely, because nothing else invalidates a
 * browse page and the deleted title never comes back to invalidate it.
 *
 * That is the whole of the "I Heart Huckabee's is still in the library" report:
 * the row was already missing = 1 in the database, and the client was reading
 * its own cache.
 */
const workFinishedKeys = [
  "libraries",
  "items",
  "item",
  "facets",
  "recently-added",
  "continue",
  "review",
  "children",
];

export function useStartScan() {
  return useBackgroundLibraryJob((id) => `/api/libraries/${id}/scan`);
}

/*
 * Scan every library at once.
 *
 * The same optimistic activity claim useBackgroundLibraryJob makes, and for the
 * same reason: the sweep answers 202 the moment the scans are started, so
 * without this the next poll might not see any active-to-idle edge and the
 * counts would never refresh. It cannot reuse that hook because it takes no
 * library id — there is no per-library scan status to invalidate, there are
 * several.
 *
 * The answer says which libraries started and which were already busy, and the
 * caller is expected to say so. "Nothing happened" and "all five were already
 * scanning" look identical otherwise.
 */
export function useScanAllLibraries() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => apiPost<ScanAllResult>("/api/libraries/scan", {}),
    onSuccess: () => {
      qc.setQueryData<ActivityStatus>(["activity"], (prev) => ({
        active: true,
        tasks: prev?.tasks ?? [],
      }));
      qc.invalidateQueries({ queryKey: ["activity"] });
      qc.invalidateQueries({ queryKey: ["scan"] });
    },
  });
}

/** Which items a metadata refresh re-asks about. */
export type RefreshScope = "all" | "unmatched";

/**
 * How many items a refresh would re-ask about, before it is asked for.
 *
 * The same shape the history reset uses, for the same reason: this is not
 * destructive but it is expensive — about 1,480 provider lookups for a real
 * film library at five a second — and a cost that only reveals itself once
 * committed is one people learn to avoid entirely. A number beforehand makes it
 * a decision instead of a gamble.
 *
 * `enabled` so the count is fetched when the menu is open and not on every
 * render of every library row: two extra requests per library on a settings
 * pane with a dozen of them is a lot of asking for a number nobody is reading.
 */
export function useRefreshPreview(
  libraryID: number,
  scope: RefreshScope,
  enabled: boolean,
) {
  return useQuery({
    queryKey: ["refresh-preview", libraryID, scope],
    queryFn: () =>
      apiGet<{ count: number; scope: RefreshScope }>(
        `/api/libraries/${libraryID}/refresh?scope=${scope}`,
      ),
    enabled,
    staleTime: 30_000,
  });
}

/**
 * Ask the provider again about one title, and everything under it.
 *
 * The endpoint has existed since metadata did and **nothing in this client ever
 * called it**: correcting one wrong title meant either Fix match, which is a
 * manual search, or refreshing the whole library — about 1,480 provider lookups
 * to fix one row.
 *
 * A show carries its episodes, which is the case that makes it worth having and
 * why the answer is a count: pressing this on a series and being told "1" says
 * its episodes are locked or unmatchable, which was previously invisible.
 */
export function useRefreshItem(id: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => apiPost<{ queued: number }>(`/api/items/${id}/refresh`, {}),
    onSuccess: () => {
      qc.setQueryData<ActivityStatus>(["activity"], (prev) => ({
        active: true,
        tasks: prev?.tasks ?? [],
      }));
      qc.invalidateQueries({ queryKey: ["activity"] });
      /*
       * The item itself, and the review queue it may leave or join. Not the
       * whole of ["items"]: a refresh changes nothing a grid renders until
       * enrichment answers, and invalidating every list to schedule background
       * work would refetch a library to show the same thing back.
       */
      qc.invalidateQueries({ queryKey: ["item", id] });
      qc.invalidateQueries({ queryKey: ["review"] });
    },
  });
}

export function useRefreshLibrary() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      libraryID,
      scope,
    }: {
      libraryID: number;
      scope: RefreshScope;
    }) =>
      // apiPost rather than apiSend: this answers with how many items it
      // requeued, and that number is the only feedback this action has ever
      // had. apiSend discards the body.
      apiPost<{ queued: number; scope: RefreshScope }>(
        `/api/libraries/${libraryID}/refresh?scope=${scope}`,
        {},
      ),
    onSuccess: (_res, { libraryID }) => {
      qc.setQueryData<ActivityStatus>(["activity"], (prev) => ({
        active: true,
        tasks: prev?.tasks ?? [],
      }));
      qc.invalidateQueries({ queryKey: ["activity"] });
      qc.invalidateQueries({ queryKey: ["scan", libraryID] });
      /*
       * The preview is now wrong, and it is the kind of wrong that is quiet:
       * every requeued row still matches the scope that named it, so a stale
       * count reads as plausible for as long as anybody looks at it.
       *
       * Not a sibling of ["refresh"], deliberately — a key reached by a prefix
       * invalidation somewhere else is the most-repeated bug in this project.
       */
      qc.invalidateQueries({ queryKey: ["refresh-preview", libraryID] });
    },
  });
}

/*
 * Re-parse is not refresh, and the button says so because the difference is not
 * obvious from either name: refresh asks the provider the same question again,
 * where this corrects the question from the filename first.
 *
 * It answers with counts rather than 202, so the caller can say what happened —
 * "98 of 160 re-parsed" is the only feedback distinguishing a run that fixed a
 * library from one that found nothing to do. Both look identical otherwise.
 *
 * The review queue is invalidated on success: requeued rows leave it as
 * enrichment resolves them, and a stale count is the one number a person
 * pressed this button to change.
 */
export function useReparseLibrary() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (libraryID: number) =>
      apiPost<ReparseResult>(`/api/libraries/${libraryID}/reparse`, {}),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["review"] });
      qc.invalidateQueries({ queryKey: ["libraries"] });
    },
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
        for (const key of workFinishedKeys) {
          qc.invalidateQueries({ queryKey: [key] });
        }
      }
      return s;
    },
    refetchInterval: (q) => (q.state.data?.state === "running" ? 1000 : false),
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
/*
 * Choose which of a collection's films it wears, or 0 to go back to the
 * default (ADR 0025's inheritance, overruled).
 *
 * Invalidates the item, the browse grids and the member list: this changes a
 * picture that is on screen in more than one place at once, and the tile in the
 * grid behind the dialog is the one somebody is looking at while they decide.
 */
export function useSetCollectionPoster(id: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (fromItemID: number) =>
      apiPost<Item>(
        `/api/items/${id}/poster`,
        { from_item_id: fromItemID },
        "PUT",
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["item", id] });
      qc.invalidateQueries({ queryKey: ["items"] });
      qc.invalidateQueries({ queryKey: ["collection-members", id] });
    },
  });
}

export function useCollectionMembers(collectionID: number, enabled: boolean) {
  return useQuery({
    queryKey: ["collection-members", collectionID],
    queryFn: ({ signal }) =>
      apiGet<ItemsPage>(
        `/api/items?collection_id=${collectionID}`,
        signal,
      ).then((r) => r.items),
    enabled: enabled && collectionID > 0,
  });
}

// Removes a title. mode "ignore" keeps the files on disk (and skips them on
// future scans); mode "delete" removes the files too; mode "forget" drops the
// row of a file that is already gone and records nothing. Every list that could
// be showing the item is refreshed afterwards.
export function useDeleteItem(id: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (mode: "ignore" | "delete" | "forget") =>
      apiSend(`/api/items/${id}?mode=${mode}`, "DELETE"),
    onSuccess: () => {
      /*
       * "libraries" was missing outright, which is what left the nav reading
       * 1,212 beside a grid that had moved on to 1,209 after three files were
       * removed. The comment above claimed every list was refreshed; it was
       * not. The grid needed naming separately too, until its key became a
       * child of ["items"] — see useInfiniteItems.
       */
      for (const key of [
        "items",
        "libraries",
        "facets",
        "recently-added",
        "continue",
        "review",
        "children",
        "collection-members",
        // The collision report is a list of items too, and forgetting a row is
        // the one action taken *from* it — a card that still shows the row it
        // just removed is the stale-view bug this project keeps shipping,
        // arriving in the one place where the person is looking straight at it.
        "collisions",
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
      return apiGet<MatchCandidate[]>(
        `/api/items/${id}/candidates${p}`,
        signal,
      );
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
    // The updated item comes back, and the caller needs it: a confirmed match
    // does not lock a row whose shape is still wrong (ADR 0041), so
    // `match_state` is how the client learns the door did not close.
    // apiPost rather than apiSend, because the caller needs the body: the
    // handler returns the updated item, and a confirmed match does not lock a
    // row whose shape is still wrong (ADR 0041), so `match_state` is how the
    // client learns the door did not close.
    mutationFn: (c: MatchCandidate) =>
      apiPost<Item>(`/api/items/${id}/match`, {
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
/*
 * Works claimed by more than one file (ADR 0042).
 *
 * Admin-only on the server, because the response carries paths. Enabled by the
 * caller rather than guarded here, so a member never fires a request that is
 * going to 403.
 */
/**
 * Record that somebody has looked at exactly these rows, or take it back.
 *
 * Not a resolution: nothing is merged, ranked or deleted and both files stay
 * where they are (ADR 0042). It answers a report that previously could not be
 * answered, which is what made it something to scroll past.
 *
 * The members are the argument rather than a handle, because a dismissal is
 * about that exact set — add a copy and it is a different collision, and the
 * server keys it that way so it comes back.
 */
export function useDismissCollision() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { itemIDs: number[]; restore?: boolean }) =>
      apiSend("/api/collisions/dismiss", "POST", {
        item_ids: v.itemIDs,
        restore: v.restore ?? false,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["collisions"] });
      // The review page shows a count of these, and a card that vanishes while
      // the heading still counts it is the stale-view bug this project keeps
      // shipping — in the one place somebody is looking straight at both.
      qc.invalidateQueries({ queryKey: ["review"] });
    },
  });
}

export function useCollisions(enabled: boolean) {
  return useQuery({
    queryKey: ["collisions"],
    queryFn: ({ signal }) =>
      apiGet<{ collisions: Collision[] }>("/api/collisions", signal),
    enabled,
  });
}

/*
 * Compare one collision's files, byte by sampled byte.
 *
 * A separate query per collision rather than a field on the list, because the
 * comparison reads three windows of every file involved and a report is opened
 * far more often than any one row in it is investigated. It runs when somebody
 * asks, and its own key caches the answer for as long as the page lives.
 *
 * `staleTime: Infinity` because the answer is about bytes on disk: it does not
 * go stale while a page is open, and re-reading 14 GB on a window focus would
 * be a surprising thing for a report to do.
 */
export function useCompareCollision(externalID: string | null) {
  return useQuery({
    queryKey: ["collisions", "compare", externalID],
    queryFn: ({ signal }) =>
      apiGet<{ collisions: Collision[] }>(
        `/api/collisions?compare=${encodeURIComponent(externalID ?? "")}`,
        signal,
      ),
    enabled: !!externalID,
    staleTime: Infinity,
  });
}

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
// The newest photographs, rather than the galleries holding them.
//
// /api/items is top-level by default, so on a picture library "recently added"
// would answer with folders — 25 galleries, the same 25 every time, which is
// not what "recently added" means to anyone looking at a photo library. An
// explicit kind is treated server-side as a deliberate cross-cutting query and
// is not forced top-level (ADR 0028).
export function useRecentPhotos(limit = 20) {
  return useQuery({
    queryKey: ["recent-photos", limit],
    queryFn: ({ signal }) =>
      apiGet<ItemsPage>(
        `/api/items?kind=photo&sort=added&limit=${limit}`,
        signal,
      ).then((r) => r.items),
    staleTime: 30_000,
  });
}

/*
 * Recently added, one window per kind of thing.
 *
 * There used to be a single query for everything, split by kind after it
 * arrived. That works until one kind arrives in bulk: importing a music library
 * of 8,882 tracks put **35 artists into the newest 40 rows**, leaving three
 * films and two collections — so the films shelf showed only what had been
 * added *since* the import, and for a while showed nothing at all and vanished.
 * Reported as exactly that.
 *
 * The window is the bug. Sorting by `added_at` across every kind means the
 * shelf that has just had a thousand rows added silently evicts every other
 * shelf, and the more recently you organised your library the emptier the page
 * looks — the opposite of what it is for.
 *
 * `useRecentPhotos` already had its own query for a version of this reason.
 * These two finish the job: a shelf asks for its own kinds, so nothing another
 * shelf does can empty it.
 *
 * The key keeps the `recently-added` prefix on purpose. Every mutation that
 * changes what a library holds invalidates that prefix, and both of these must
 * be caught by it.
 */

// Music and pictures have shelves of their own, so the video window excludes
// them rather than filtering them out after the fact — filtering after the
// fact is what made the window the wrong size in the first place.
const NOT_VIDEO = "artist,album,track,gallery,photo";

export function useRecentlyAddedVideo(limit = 20) {
  return useQuery({
    queryKey: ["recently-added", "video", limit],
    queryFn: ({ signal }) =>
      apiGet<ItemsPage>(
        `/api/items?sort=added&limit=${limit}&exclude_kind=${NOT_VIDEO}`,
        signal,
      ).then((r) => r.items),
    staleTime: 30_000,
  });
}

/*
 * Artists rather than albums or tracks: this list is top-level, and for a music
 * library the top level is the artist (ADR 0024). Asking for the kind directly
 * says so, where relying on the shape of a general query only appeared to.
 */
export function useRecentlyAddedMusic(limit = 20) {
  return useQuery({
    queryKey: ["recently-added", "music", limit],
    queryFn: ({ signal }) =>
      apiGet<ItemsPage>(
        `/api/items?kind=artist&sort=added&limit=${limit}`,
        signal,
      ).then((r) => r.items),
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
      apiSend(
        `/api/items/${id}/subtitles/${encodeURIComponent(key)}`,
        "DELETE",
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["subtitles", id] }),
  });
}

export interface ItemQuery {
  libraryID: number;
  /** Restrict to one kind — the collections page asks for exactly those. */
  kind?: string;
  q?: string;
  sort?: string; // title | year | added
  genres?: string[];
  decades?: number[];
  contentRatings?: string[];
  unwatched?: boolean;
  years?: number[];
  /** Resolution bucket keys — uhd | hd1080 | hd720 | sd. */
  resolutions?: string[];
  /** Person ids from /cast. Matches any credited role. */
  people?: number[];
  /** Person ids restricted to acting credits. */
  actors?: number[];
  /** Person ids restricted to directing credits. */
  directors?: number[];
  /** Collection ids; membership, not parenthood. */
  collections?: number[];
  /** Rated at least this highly, out of ten. Unrated items are excluded. */
  minRating?: number;
  /** in_progress | unmatched. Single-valued; the two cannot be combined. */
  status?: string;
  /** Drop one kind from the listing — the grid uses it for collections. */
  excludeKind?: string;
  /** A–Z rail: one letter, or "#" for everything that starts with anything else. */
  initial?: string;
  limit?: number;
  offset?: number;
}

// itemsParams builds the grid query string. Shared by the single-page and
// paging hooks so the two can never disagree about how a filter is encoded.
function itemsParams({
  libraryID,
  kind,
  q,
  sort,
  genres = [],
  decades = [],
  contentRatings = [],
  unwatched = false,
  years = [],
  resolutions = [],
  people = [],
  actors = [],
  directors = [],
  collections = [],
  minRating = 0,
  status,
  excludeKind,
  initial,
  limit = 120,
  offset = 0,
}: ItemQuery): URLSearchParams {
  const params = new URLSearchParams({
    library_id: String(libraryID),
    limit: String(limit),
    offset: String(offset),
  });
  if (kind) params.set("kind", kind);
  if (q) params.set("q", q);
  if (sort) params.set("sort", sort);
  if (excludeKind) params.set("exclude_kind", excludeKind);
  if (initial) params.set("initial", initial);
  // Repeatable facet filters — one param per chosen value (OR within a facet).
  for (const g of genres) params.append("genre", g);
  for (const d of decades) params.append("decade", String(d));
  for (const c of contentRatings) params.append("content_rating", c);
  for (const y of years) params.append("year", String(y));
  for (const r of resolutions) params.append("resolution", r);
  for (const p of people) params.append("person", String(p));
  for (const a of actors) params.append("actor", String(a));
  for (const d of directors) params.append("director", String(d));
  for (const c of collections) params.append("collection", String(c));
  if (minRating > 0) params.set("min_rating", String(minRating));
  if (status) params.set("status", status);
  if (unwatched) params.set("watched", "false");
  return params;
}

/**
 * Search every library at once.
 *
 * The server has always been able to do this — ItemFilter only constrains
 * library_id when it is non-zero — and nothing ever asked. Searching was
 * per-library, which means knowing which library a thing is in before you can
 * look for it, which is the opposite of what search is for.
 *
 * Top-level rows only, the same as a browse grid: a search that returns
 * episodes loose among films answers a question nobody asked, and the episode's
 * show is the thing you wanted.
 */
export function useGlobalSearch(q: string) {
  const query = q.trim();
  return useQuery({
    queryKey: ["search", query],
    queryFn: ({ signal }) =>
      apiGet<ItemsPage>(
        `/api/items?q=${encodeURIComponent(query)}&limit=60`,
        signal,
      ),
    enabled: query.length >= 2,
    // A search that flashes empty between keystrokes reads as "no results".
    placeholderData: keepPreviousData,
  });
}

/*
 * The photo timeline: counts by capture month, newest first (ADR 0028's
 * `taken_at`, not `added_at`).
 *
 * One small response describes the whole library's shape, and a month's
 * photographs are fetched only when that month is opened — 3,676 photographs is
 * a page nobody wants.
 */
export type TimelineBucket = {
  year: number;
  month: number;
  undated?: boolean;
  count: number;
};

export function usePhotoTimeline(libraryID: number, enabled = true) {
  return useQuery({
    queryKey: ["timeline", libraryID],
    queryFn: ({ signal }) =>
      apiGet<{ buckets: TimelineBucket[]; total: number }>(
        `/api/libraries/${libraryID}/timeline`,
        signal,
      ),
    enabled: enabled && libraryID > 0,
  });
}

/*
 * One month of it. `undated` is its own bucket rather than a missing month —
 * 5% of a real library carries no capture time, and dropping them would lose
 * them silently.
 */
export function usePhotosInMonth(
  libraryID: number,
  bucket: TimelineBucket | null,
) {
  const params = new URLSearchParams({
    library_id: String(libraryID),
    kind: "photo",
    sort: "taken",
    limit: "500",
  });
  if (bucket?.undated) {
    params.set("taken_undated", "1");
  } else if (bucket) {
    params.set(
      "taken_month",
      `${bucket.year}-${String(bucket.month).padStart(2, "0")}`,
    );
  }
  return useQuery({
    queryKey: ["items", params.toString()],
    queryFn: ({ signal }) =>
      apiGet<ItemsPage>(`/api/items?${params.toString()}`, signal),
    enabled: libraryID > 0 && bucket !== null,
  });
}

/*
 * People — face groups in a picture library (ADR 0052).
 *
 * `pending` travels with the list on purpose. An empty people list means either
 * "nobody is in your photographs" or "nothing has looked at them yet", and a
 * screen that cannot tell those apart teaches somebody the feature is broken.
 */
/*
 * Named FacePerson rather than Person because `Person` in this client is
 * already an account — the sharing sense of the word. Two Persons in one
 * codebase is a bug waiting for whoever imports the wrong one.
 */
export type FacePerson = {
  id: number;
  name: string | null;
  name_locked: boolean;
  count: number;
  cover_face_id?: number;
  cover_item_id?: number;
};

export function useFacePeople(libraryID: number, enabled = true) {
  return useQuery({
    queryKey: ["people-faces", libraryID],
    queryFn: ({ signal }) =>
      apiGet<{ people: FacePerson[]; pending: number }>(
        `/api/libraries/${libraryID}/people`,
        signal,
      ),
    enabled: enabled && libraryID > 0,
  });
}

/** Whether this server can group faces at all, and why not when it cannot. */
export function useFaceCapabilities(enabled = true) {
  return useQuery({
    queryKey: ["face-capabilities"],
    queryFn: ({ signal }) =>
      apiGet<{ ready: boolean; reason?: string; version?: string }>(
        "/api/faces/capabilities",
        signal,
      ),
    enabled,
    staleTime: 60_000,
  });
}

/** One group's faces, clearest first — the examples a naming screen shows. */
export function useClusterFaces(clusterID: number | null) {
  return useQuery({
    queryKey: ["cluster-faces", clusterID],
    queryFn: ({ signal }) =>
      apiGet<{ faces: { id: number; item_id: number; score: number }[] }>(
        `/api/faces/clusters/${clusterID}/faces`,
        signal,
      ),
    enabled: clusterID !== null,
  });
}

/*
 * Naming a group.
 *
 * Invalidates the people list rather than the group: a name changes the label
 * on a tile in a grid somebody is looking at, and this project's most-repeated
 * bug is a write that is right on the server and stale on the screen.
 */
export function useNamePerson(libraryID: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, name }: { id: number; name: string }) =>
      apiSend(`/api/faces/clusters/${id}`, "PUT", { name }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["people-faces", libraryID] });
    },
  });
}

/** Start a face pass over a picture library. Progress arrives via activity. */
export function useStartFacePass(libraryID: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => apiSend(`/api/libraries/${libraryID}/faces`, "POST"),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["activity"] });
      qc.invalidateQueries({ queryKey: ["people-faces", libraryID] });
    },
  });
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

/*
 * The Cast filter's type-ahead.
 *
 * Keyed on the query so each distinct search is cached separately — typing
 * back to something already asked for is answered from memory rather than by
 * asking again. `keepPreviousData` is deliberate: without it the list empties
 * between keystrokes and the panel flickers through blank on every letter.
 */
export function useCast(libraryID: number, query: string, role = "") {
  return useQuery({
    queryKey: ["cast", libraryID, query, role],
    queryFn: ({ signal }) =>
      apiGet<{ people: CastMember[] }>(
        `/api/libraries/${libraryID}/cast?q=${encodeURIComponent(query)}` +
          (role ? `&role=${encodeURIComponent(role)}` : ""),
        signal,
      ),
    enabled: libraryID > 0,
    placeholderData: (prev) => prev,
    staleTime: 30_000,
  });
}

/*
 * The people behind an active filter, by id.
 *
 * Needed because filter state lives in the URL: a bookmarked `?person=12` has
 * an id and no name, and a pill reading "person 12" is not a filter anybody can
 * read. Fetched separately from the search so a pill survives a reload without
 * the search panel ever having been opened.
 */
export function useCastByIDs(libraryID: number, ids: string[]) {
  const key = ids.join(",");
  return useQuery({
    queryKey: ["cast-by-id", libraryID, key],
    queryFn: ({ signal }) =>
      apiGet<{ people: CastMember[] }>(
        `/api/libraries/${libraryID}/cast?` +
          ids.map((i) => `id=${i}`).join("&"),
        signal,
      ),
    enabled: libraryID > 0 && ids.length > 0,
    staleTime: 300_000,
  });
}

/*
 * Fetching ffmpeg (ADR 0043).
 *
 * Polled while an install runs and left alone when it is not: this is a
 * two-minute download whose progress a spinner cannot express, and a status
 * nobody is watching is not worth a request every second.
 */
export function useMediaTools(enabled: boolean) {
  return useQuery({
    queryKey: ["media-tools"],
    queryFn: ({ signal }) =>
      apiGet<MediaToolsState>("/api/media-tools", signal),
    enabled,
    refetchInterval: (q) => (q.state.data?.running ? 1000 : false),
  });
}

export function useInstallMediaTools() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => apiSend("/api/media-tools/install", "POST"),
    // Invalidate both: the install changes what the server can do, which the
    // settings payload reports separately from this job's progress.
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["media-tools"] });
      qc.invalidateQueries({ queryKey: ["settings"] });
    },
  });
}

export function useCancelMediaToolsInstall() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => apiSend("/api/media-tools/install/cancel", "POST"),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["media-tools"] }),
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
      apiGet<{ plugins: Plugin[] }>("/api/plugins", signal).then(
        (r) => r.plugins,
      ),
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
      apiPost<Plugin>(
        `/api/plugins/${encodeURIComponent(args.name)}/grant`,
        args.caps,
      ),
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
    /*
     * A child of ["items"], not a sibling.
     *
     * This key was ["items-infinite", …]. React Query matches by key *prefix*,
     * and "items-infinite" is a different string from "items" rather than a
     * child of it — so every `invalidateQueries({ queryKey: ["items"] })` in
     * this file sailed straight past the browse grid. Editing a title, deleting
     * a file, finishing a scan: the grid kept its cached pages through all of
     * it, which is how a deleted film stayed on screen after the scan had
     * already marked it missing in the database.
     *
     * One call site had noticed and listed both strings by hand. The rest had
     * not, and a convention that has to be remembered at every call site is a
     * convention that will be forgotten at the next one. Nesting it makes the
     * obvious invalidation the correct one.
     */
    queryKey: ["items", "infinite", base.toString()],
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
        for (const key of workFinishedKeys) {
          qc.invalidateQueries({ queryKey: [key] });
        }
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
/**
 * Throw away something the server can make again.
 *
 * Deliberately narrow: the only targets are caches. Everything reachable here
 * costs time and provider requests to rebuild, never information.
 */
export function useClearCache() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (target: "artwork" | "transcode") =>
      apiPost<{ freed_bytes: number }>("/api/cache/clear", { target }),
    onSuccess: () => {
      // Artwork URLs are content-addressed and unchanged, but everything on
      // screen is now pointing at bytes that are gone until a refresh.
      qc.invalidateQueries({ queryKey: ["items"] });
      qc.invalidateQueries({ queryKey: ["item"] });
    },
  });
}

/** Restore the documented defaults. Credentials and machine facts survive. */
export function useResetSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => apiPost<unknown>("/api/settings/reset", {}),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["settings"] }),
  });
}

export function useUpdateStatus(enabled = true, watch = false) {
  return useQuery({
    queryKey: ["update"],
    queryFn: ({ signal }) => apiGet<UpdateStatus>("/api/update", signal),
    // Admin-only endpoint: a member asking gets a 401, so the shell's banner
    // passes false rather than filling the console with refused requests on
    // every page load.
    enabled,
    /*
     * `watch` is for the window between "download started" and "staged".
     *
     * POST /api/update/download returns immediately and downloads in the
     * background — deliberately, since it is not a request to hold open. But
     * this query was cached for a minute and never refetched, so the panel kept
     * showing "Downloading…" indefinitely while the activity indicator, which
     * polls, already said the update was ready. Two surfaces reading the same
     * server and disagreeing, which reads as a hang.
     */
    staleTime: watch ? 0 : 60_000,
    refetchInterval: watch ? 2000 : false,
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

// Finishes a staged update by restarting the server.
//
// The server spawns a detached helper and then goes down, so this request often
// does not get a response at all — the connection dies with the process that was
// answering it. A dropped connection here is success, not failure, which is why
// the error is swallowed rather than shown: telling someone their restart failed
// while it is working is worse than saying nothing.
export function useRestartForUpdate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      try {
        return await apiSend("/api/update/restart", "POST");
      } catch (e) {
        if (e instanceof TypeError) return null; // connection dropped: the restart began
        throw e;
      }
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["update"] });
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

/**
 * fetchArtistQueue flattens an artist into a play queue: every track of every
 * album, albums in the order the artist page lists them and tracks in the order
 * the record plays.
 *
 * **An artist's children are usually albums, and not always.** A file sitting
 * in an artist's folder with no album folder around it is parsed as a track
 * belonging to that artist and nothing else, so it is parented straight to the
 * artist — reported on a real library as two Play all buttons on one page, one
 * of which did nothing.
 *
 * Those loose tracks are queued where they sit. Taking the children in the
 * order the page lists them and expanding only the albums keeps the queue in
 * the order somebody is looking at, which no amount of grouping afterwards
 * recovers.
 *
 * It is a fetch rather than a hook because it runs when the button is pressed,
 * not when the page opens. A prolific artist is thirty albums, and thirty
 * requests to render a page whose Play all may never be pressed is a bad
 * trade — where thirty requests on the press, once, cached afterwards by the
 * same query keys the album pages use, costs nothing anybody notices on a LAN.
 */
export async function fetchArtistQueue(
  qc: QueryClient,
  children: { id: number; kind: string }[],
): Promise<number[]> {
  const pages = await Promise.all(
    children.map((child) =>
      // A track under the artist is already a leaf. Asking the server for its
      // children would be a request that can only answer "none", once per
      // single, on every press.
      child.kind !== "album"
        ? Promise.resolve([child as unknown as Item])
        : qc.fetchQuery({
            // The same key useChildren uses for an album, so a record already
            // opened is already here, and opening one later is served from this.
            queryKey: ["children", child.id, "track"],
            queryFn: () =>
              apiGet<ItemsPage>(`/api/items?parent_id=${child.id}&sort=track`).then(
                (r) => r.items,
              ),
          }),
    ),
  );
  // Only leaves. An album containing anything other than tracks is not a shape
  // the scanner produces today, but handing the player a container would be a
  // silent failure rather than a loud one.
  return pages
    .flat()
    .filter((t) => !isContainer(t))
    .map((t) => t.id);
}

/**
 * fetchDescendantIDs flattens any container into a play queue of leaf ids.
 *
 * fetchArtistQueue above is the two-level version of this for one shape; this
 * is the general one, and it exists because a right-click on a *tile* has no
 * page open behind it. The detail page knows a show's seasons because it
 * rendered them. A menu opened on a poster in a search result knows nothing but
 * the row, so "Play all" has to go and find out.
 *
 * Depth-first, so the order is the order the thing is meant to be consumed in:
 * season one before season two, and the tracks of a record in the order the
 * record plays. Breadth-first would queue every season's first episode.
 *
 * The query key is useChildren's, so a container whose page has been opened is
 * already in cache and this costs nothing -- and opening it afterwards is
 * served from what this fetched.
 *
 * A collection is deliberately *not* handled here. Its membership lives in
 * item_collection rather than parent_id, so a collection has no children to
 * walk and the caller uses collection_id instead.
 *
 * **A show is not handled here either, and callers must not send one.** Its
 * episodes hang off seasons, and `/api/items/{id}/episodes` exists precisely
 * so that no client reimplements that walk -- showplay.go says so in as many
 * words, including the case this would get wrong: a show whose episodes sit
 * directly under the show row rather than under a season. That endpoint also
 * excludes missing episodes and orders by season and episode outright, rather
 * than relying on every episode tying on sort_title for the default sort to
 * fall through. This is for the shapes with no dedicated endpoint.
 */
export async function fetchDescendantIDs(
  qc: QueryClient,
  parentID: number,
  depth = 0,
): Promise<number[]> {
  // A cycle in parent_id is not a shape the scanner produces, but a database
  // row is trusted data and this is a recursive walk over it: without a stop,
  // one bad row is a hung tab rather than a wrong answer. Four is deeper than
  // any real hierarchy here (artist -> album -> track is three).
  if (depth > 4) return [];

  /*
   * Paged, because the API caps `limit` and defaults it to 100.
   *
   * The first version asked once and took what came back, which is the exact
   * silent truncation fetchLibraryTracks carries a comment about: a container
   * with more than a hundred children would produce a Play all that worked
   * perfectly and quietly left out the rest. Rare in a season and entirely
   * ordinary in a gallery.
   */
  const PAGE = 500;
  const kids: Item[] = [];
  for (let offset = 0; ;) {
    const page = await qc.fetchQuery({
      queryKey: ["children", parentID, "", offset],
      queryFn: () =>
        apiGet<ItemsPage>(
          `/api/items?parent_id=${parentID}&limit=${PAGE}&offset=${offset}`,
        ),
    });
    kids.push(...page.items);
    offset += page.items.length;
    if (page.items.length === 0 || offset >= page.total) break;
  }

  const ids: number[] = [];
  for (const kid of kids) {
    if (isContainer(kid)) {
      ids.push(...(await fetchDescendantIDs(qc, kid.id, depth + 1)));
    } else if (!kid.missing) {
      /*
       * A missing item is a row whose file is gone -- an unmounted drive, a
       * deleted file the scan has seen but not been told to forget. Queueing
       * one hands the player something it cannot open, which stalls the queue
       * at that position rather than skipping it. EpisodesOf filters these
       * server-side; this walk was not.
       */
      ids.push(kid.id);
    }
  }
  return ids;
}

/**
 * fetchLibraryTracks returns every track id in a library, in title order.
 *
 * Pages rather than asking for everything at once: `limit` is capped at 500 by
 * the API, and a library larger than that would otherwise be silently truncated
 * — the worst kind of bug, because "Play all" would work perfectly and quietly
 * leave out the last nine thousand songs.
 */
export async function fetchLibraryTracks(
  qc: QueryClient,
  libraryID: number,
  kind: PlayableKind = "track",
): Promise<number[]> {
  const PAGE = 500;
  const ids: number[] = [];
  let offset = 0;
  for (;;) {
    const params = new URLSearchParams({
      library_id: String(libraryID),
      kind,
      // Title order for everything, which for episodes means the server's
      // sort_title, season, episode — a show library queued in the order it is
      // meant to be watched rather than alphabetically by episode name.
      sort: "title",
      limit: String(PAGE),
      offset: String(offset),
    });
    const page = await qc.fetchQuery({
      queryKey: ["library-tracks", libraryID, kind, offset],
      queryFn: () => apiGet<ItemsPage>(`/api/items?${params.toString()}`),
    });
    ids.push(...page.items.map((t) => t.id));
    offset += page.items.length;
    // Stop on the server's own total, and stop on an empty page regardless —
    // a total that disagrees with what is returned must not become a loop that
    // never ends.
    if (page.items.length === 0 || offset >= page.total) break;
  }
  return ids;
}

/*
 * The leaf kind a library's "play all" queues.
 *
 * A show library queues **episodes**, not shows: a queue of containers is not
 * something a player can advance through, and "play all" over a show library
 * means the episodes in order. Pictures are deliberately absent — a slideshow is
 * a different control with a different pace, and pretending it is a queue would
 * ship the wrong thing quickly.
 */
export type PlayableKind = "track" | "movie" | "episode";

export function playableKindFor(
  libraryKind: string | undefined,
): PlayableKind | null {
  switch (libraryKind) {
    case "music":
      return "track";
    case "show":
      return "episode";
    case "picture":
    case "other":
      return null;
    default:
      return "movie";
  }
}

/*
 * A show's play actions, fetched on the press rather than held in a cache.
 *
 * Plain functions, not useQuery, and that is the design rather than laziness.
 * The bug this feature exists to avoid is a stale read — pressing continue and
 * being sent to an episode already watched — and a react-query cache is exactly
 * the thing that would reintroduce it, intermittently, on whatever staleTime it
 * happened to be given. The server says no-store; the client does not keep it
 * either.
 */
export async function fetchShowContinue(showID: number): Promise<{
  episode?: Item;
  resume: boolean;
  exhausted: boolean;
}> {
  return apiGet(`/api/items/${showID}/continue`);
}

export async function fetchShowEpisodes(showID: number): Promise<Item[]> {
  const res = await apiGet<{ episodes: Item[] }>(
    `/api/items/${showID}/episodes`,
  );
  return res.episodes ?? [];
}

/*
 * Marking an episode watched, or putting it back.
 *
 * The server already takes both through `PUT /api/items/{id}/progress`, and
 * applies its own rule on the way in: `watched := req.Watched || past the
 * threshold`. So marking watched is `watched: true`, and clearing it is
 * `position_ms: 0` with `watched: false` — a position of zero is the only
 * honest way to say "I have not seen this", because leaving the position
 * behind would put the row straight back on the Continue shelf.
 *
 * Invalidates the children list and the item, which is what redraws the row and
 * anything else showing that episode's state. Continue is deliberately *not*
 * invalidated: it is never cached (ADR-less by design, see showplay.go), so
 * there is nothing to clear.
 */
/*
 * The same two writes, for a caller with no parent to invalidate.
 *
 * A Continue Watching tile is not inside a season — it is a film, or an episode
 * a long way from the show page it belongs to — so `["children", parentID]` is
 * a key it cannot supply and would not want cleared. Everything else about the
 * two operations is identical, which is why this delegates rather than
 * restating them: two copies of "position_ms: 0, watched: false" would be two
 * places to get the meaning of *unwatched* wrong.
 */
export function useSetWatchedByID() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { itemID: number; watched: boolean }) =>
      apiSend(`/api/items/${args.itemID}/progress`, "PUT", {
        position_ms: 0,
        watched: args.watched,
      }),
    onSuccess: (_data, args) => {
      qc.invalidateQueries({ queryKey: ["item", args.itemID] });
      qc.invalidateQueries({ queryKey: ["continue"] });
      // A tile leaving the Continue shelf changes what the hero shows, and the
      // hero is drawn from the same list.
      qc.invalidateQueries({ queryKey: ["recently-added"] });
      /*
       * And the browse grid, which is where this is now called from.
       *
       * Marking something watched clears its saved position, so the progress
       * bar on its tile should go — and on a grid filtered to unwatched, the
       * tile itself should. Neither happens without this: the grid's key is
       * ["items", "infinite", …] and nothing else here reaches it.
       *
       * This is the same shape as the bug that kept a deleted film on screen
       * for a whole release. Worth naming, because the failure is quiet: the
       * write succeeds, the server is right, and only the picture is stale.
       */
      qc.invalidateQueries({ queryKey: ["items"] });
    },
  });
}

export function useSetWatched(parentID: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { itemID: number; watched: boolean }) =>
      apiSend(`/api/items/${args.itemID}/progress`, "PUT", {
        position_ms: 0,
        watched: args.watched,
      }),
    onSuccess: (_data, args) => {
      qc.invalidateQueries({ queryKey: ["children", parentID] });
      qc.invalidateQueries({ queryKey: ["item", args.itemID] });
      // The Continue Watching shelf reads the same state, and a season page is
      // exactly where somebody corrects it after watching an episode elsewhere.
      // The key matches useContinueWatching's, which is ["continue", limit] —
      // a prefix invalidation covers every limit in play.
      qc.invalidateQueries({ queryKey: ["continue"] });
    },
  });
}

// Server identity. /api/health has always returned this and nothing has ever
// asked — so the settings page could not say which version it was talking to.
export function useHealth(watch = false) {
  return useQuery({
    queryKey: ["health"],
    queryFn: ({ signal }) =>
      apiGet<{ status: string; version: string; api_version: number }>(
        "/api/health",
        signal,
      ),
    // `watch` is for the one moment the server is expected to stop answering
    // and then answer again: finishing an update. Polling fast and retrying
    // hard is right there and wrong everywhere else, which is why it is a
    // parameter rather than the default.
    staleTime: watch ? 0 : 60_000,
    refetchInterval: watch ? 1000 : false,
    retry: watch ? 60 : 3,
    retryDelay: 1000,
  });
}

/**
 * A playlist's entries, in playing order.
 *
 * Not useChildren: a playlist's members live in playlist_entry rather than
 * under parent_id, because a track belongs to its album and being in a playlist
 * does not move it (ADR 0030).
 */
export function usePlaylistEntries(playlistID: number, enabled: boolean) {
  return useQuery({
    queryKey: ["playlist-entries", playlistID],
    queryFn: ({ signal }) =>
      apiGet<ItemsPage>(`/api/items?playlist_id=${playlistID}`, signal).then(
        (r) => r.items,
      ),
    enabled: enabled && playlistID > 0,
  });
}

/**
 * Every playlist on the server, for the "add to playlist" menu.
 *
 * A plain item listing filtered to the kind, so it picks up the same sort and
 * artwork every other grid gets. Playlists are server-wide (ADR 0030), so there
 * is no per-user filtering to do here yet.
 */
export function usePlaylists(libraryID?: number, enabled = true) {
  // Scoped to a library when one is given, because a playlist belongs to the
  // library its tracks and its .m3u live in and the playlists page is a page
  // *of* a library. The picker passes nothing and gets them all: when you are
  // adding a track to a list, "which library is this list filed in" is a
  // question about the database, not about music.
  const scope = libraryID ? `&library_id=${libraryID}` : "";
  return useQuery({
    queryKey: ["playlists", libraryID ?? 0],
    queryFn: ({ signal }) =>
      apiGet<ItemsPage>(
        `/api/items?kind=playlist&limit=200${scope}`,
        signal,
      ).then((r) => r.items),
    enabled,
    staleTime: 10_000,
  });
}

/**
 * Rename a playlist.
 *
 * PATCH /api/items/{id} — the ordinary metadata edit, which has accepted a
 * title since M2 and which no client has ever called for a playlist. Editing a
 * field locks it, which is the point: a name someone typed is not something a
 * later provider refresh or rescan may overwrite.
 */
export function useRenamePlaylist(id: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (title: string) =>
      apiSend(`/api/items/${id}`, "PATCH", { title }),
    onSuccess: () => invalidatePlaylists(qc, id),
  });
}

/**
 * Invalidate everything a playlist edit can be seen through.
 *
 * One helper rather than a list per mutation: a playlist appears in its own
 * entry list, in the playlist picker, and in the grids and shelves that list
 * items — and an edit that refreshes two of those three leaves the third
 * showing a playlist that no longer exists.
 */
function invalidatePlaylists(qc: QueryClient, playlistID?: number) {
  if (playlistID) {
    qc.invalidateQueries({ queryKey: ["playlist-entries", playlistID] });
    qc.invalidateQueries({ queryKey: ["item", playlistID] });
  }
  for (const key of ["playlists", "items", "recently-added"]) {
    qc.invalidateQueries({ queryKey: [key] });
  }
}

export function useCreatePlaylist() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { title: string; library_id: number }) =>
      apiPost<Item>("/api/playlists", v),
    onSuccess: () => invalidatePlaylists(qc),
  });
}

export function useDeletePlaylist(id: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => apiSend(`/api/playlists/${id}`, "DELETE"),
    onSuccess: () => invalidatePlaylists(qc, id),
  });
}

/** Append to the end of a playlist — "add to playlist". */
export function useAddToPlaylist() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { playlistID: number; itemIDs: number[] }) =>
      apiSend(`/api/playlists/${v.playlistID}/entries`, "POST", {
        item_ids: v.itemIDs,
      }),
    onSuccess: (_r, v) => invalidatePlaylists(qc, v.playlistID),
  });
}

/**
 * Replace the whole sequence — a reorder.
 *
 * The server takes the entire order rather than a move instruction, because a
 * playlist is an ordered sequence and the client already knows what it should
 * be. Sending the list it just rendered is also what makes a repeated track
 * survive a reorder: the ids go as they are, duplicates and all.
 */
export function useSetPlaylistEntries(id: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (itemIDs: number[]) =>
      apiSend(`/api/playlists/${id}/entries`, "PUT", { item_ids: itemIDs }),
    onSuccess: () => invalidatePlaylists(qc, id),
  });
}

/**
 * Remove one entry, addressed by position.
 *
 * Not by item id: a playlist may hold the same track twice, so an id does not
 * name a row. The position is the index the list rendered, which is why the
 * server keeps them dense.
 */
export function useRemovePlaylistEntry(id: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (position: number) =>
      apiSend(`/api/playlists/${id}/entries/${position}`, "DELETE"),
    onSuccess: () => invalidatePlaylists(qc, id),
  });
}

// ------------------------------------------------------------- profile

/*
 * The profile page's one request.
 *
 * Paged rather than infinite: a history is read from the top and abandoned, and
 * an infinite query for a list nobody scrolls to the end of buys nothing but a
 * cache that never shrinks. `has_more` from the server drives the button.
 */
export function useProfile(limit = 50, offset = 0) {
  return useQuery({
    queryKey: ["profile", limit, offset],
    queryFn: ({ signal }) =>
      apiGet<Profile>(`/api/profile?limit=${limit}&offset=${offset}`, signal),
    placeholderData: keepPreviousData,
    staleTime: 10_000,
  });
}

// ------------------------------------------------------------- crashes

// Fetched only when the pane is open. A crash list polled in the background is
// a request per interval to answer "still none", forever.
export function useCrashes(enabled: boolean) {
  return useQuery({
    queryKey: ["crashes"],
    queryFn: ({ signal }) =>
      apiGet<{ crashes: CrashReport[] }>("/api/crashes", signal),
    enabled,
    staleTime: 0,
  });
}

export function useClearCrashes() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => apiSend("/api/crashes", "DELETE"),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["crashes"] }),
  });
}

// ------------------------------------------------------------ trending

/*
 * A library's recent activity.
 *
 * `staleTime` is generous: this changes when somebody finishes something, which
 * is minutes-to-hours scale, and a shelf that refetches on every navigation
 * would be a query per page view for a list that had not moved.
 */
export function useTrending(libraryID: number | undefined, limit = 12) {
  return useQuery({
    queryKey: ["trending", libraryID, limit],
    queryFn: ({ signal }) =>
      apiGet<Trending>(
        `/api/libraries/${libraryID}/trending?limit=${limit}`,
        signal,
      ),
    enabled: !!libraryID,
    staleTime: 5 * 60_000,
  });
}

// -------------------------------------------------------------- ratings

export function useRating(itemID: number | undefined) {
  return useQuery({
    queryKey: ["rating", itemID],
    queryFn: ({ signal }) =>
      apiGet<{ rating: Rating | null }>(`/api/items/${itemID}/rating`, signal),
    enabled: !!itemID,
    staleTime: 30_000,
  });
}

export function useSetRating(itemID: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { score: number; review?: string }) =>
      apiSend(`/api/items/${itemID}/rating`, "PUT", body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["rating", itemID] });
      qc.invalidateQueries({ queryKey: ["my-ratings"] });
    },
  });
}

// Withdrawing is its own mutation, not a score of zero: "I have not rated this"
// and "I rated this badly" are different statements and the API keeps them so.
export function useClearRating(itemID: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => apiSend(`/api/items/${itemID}/rating`, "DELETE"),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["rating", itemID] });
      qc.invalidateQueries({ queryKey: ["my-ratings"] });
    },
  });
}

export function useMyRatings(limit = 50) {
  return useQuery({
    queryKey: ["my-ratings", limit],
    queryFn: ({ signal }) =>
      apiGet<{ ratings: RatedItem[] }>(
        `/api/profile/ratings?limit=${limit}`,
        signal,
      ),
    staleTime: 30_000,
  });
}

// ------------------------------------------------------------- profile edit

export function useRenameSelf() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (name: string) => apiSend("/api/profile", "PATCH", { name }),
    onSuccess: () => {
      // The name is in the auth status the whole shell reads, so both go.
      qc.invalidateQueries({ queryKey: ["auth-status"] });
      qc.invalidateQueries({ queryKey: ["profile"] });
    },
  });
}

export function useUpdateUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      ...patch
    }: {
      id: string;
      name?: string;
      role?: Role;
    }) => apiSend(`/api/users/${id}`, "PATCH", patch),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });
}

// ------------------------------------------------------------- live tv

export function useChannels(sourceID?: number) {
  return useQuery({
    queryKey: ["channels", sourceID ?? 0],
    queryFn: ({ signal }) =>
      apiGet<{ channels: Channel[] }>(
        `/api/channels${sourceID ? `?source_id=${sourceID}` : ""}`,
        signal,
      ),
    staleTime: 60_000,
  });
}

export function useChannelSources(enabled: boolean) {
  return useQuery({
    queryKey: ["channel-sources"],
    queryFn: ({ signal }) =>
      apiGet<{ sources: ChannelSource[] }>("/api/channel-sources", signal),
    enabled,
    staleTime: 30_000,
  });
}

export function useAddChannelSource() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { name: string; url: string; epg_url?: string }) =>
      apiPost<{
        source: ChannelSource;
        channels?: number;
        programs?: number;
        import_error?: string;
        epg_error?: string;
      }>("/api/channel-sources", body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["channel-sources"] });
      qc.invalidateQueries({ queryKey: ["channels"] });
    },
  });
}

export function useRefreshChannelSource() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) =>
      apiSend(`/api/channel-sources/${id}/refresh`, "POST"),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["channel-sources"] });
      qc.invalidateQueries({ queryKey: ["channels"] });
    },
  });
}

export function useDeleteChannelSource() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => apiSend(`/api/channel-sources/${id}`, "DELETE"),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["channel-sources"] });
      qc.invalidateQueries({ queryKey: ["channels"] });
      qc.invalidateQueries({ queryKey: ["guide"] });
    },
  });
}

// Setting a guide URL imports it there and then, so the answer carries the
// count — and the failure, separately from the channel list's.
export function useSetGuideURL() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: number; epg_url: string }) =>
      apiPost<{ programs?: number; epg_error?: string }>(
        `/api/channel-sources/${v.id}`,
        { epg_url: v.epg_url },
        "PATCH",
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["channel-sources"] });
      qc.invalidateQueries({ queryKey: ["guide"] });
    },
  });
}

/*
 * What is on now, across every channel.
 *
 * One request for the whole page rather than one per channel: a six-hundred
 * channel list would otherwise open with six hundred round trips to fill a
 * strapline.
 *
 * Refetched every five minutes and on window focus. A guide is wrong by the
 * clock rather than by an event — nothing tells the client that the eight
 * o'clock programme has started — so the only thing that keeps a strapline
 * honest is asking again. Five minutes is close enough that a wrong "on now"
 * is rare and infrequent enough to cost nothing.
 */
export function useGuide() {
  return useQuery({
    queryKey: ["guide"],
    queryFn: ({ signal }) =>
      apiGet<{ at: number; channels: Record<string, GuideNow> }>(
        "/api/guide",
        signal,
      ),
    staleTime: 5 * 60_000,
    refetchInterval: 5 * 60_000,
  });
}

// One channel's schedule, fetched only while somebody is looking at it.
export function useChannelSchedule(channelID: number | null, hours = 12) {
  return useQuery({
    queryKey: ["channel-guide", channelID, hours],
    queryFn: ({ signal }) =>
      apiGet<{ programs: Program[] }>(
        `/api/channels/${channelID}/guide?hours=${hours}`,
        signal,
      ),
    enabled: channelID !== null,
    staleTime: 5 * 60_000,
  });
}

// -------------------------------------------------------------- people

export function usePeople() {
  return useQuery({
    queryKey: ["people"],
    queryFn: ({ signal }) =>
      apiGet<{ people: Person[] }>("/api/people", signal),
    staleTime: 60_000,
  });
}

// Enabled only when that person shares — the endpoint answers an empty list
// either way, and asking anyway would be a request whose answer is known.
export function usePersonActivity(id: string | undefined, sharing: boolean) {
  return useQuery({
    queryKey: ["person-activity", id],
    queryFn: ({ signal }) =>
      apiGet<{ activity: HistoryEntry[] }>(
        `/api/people/${id}/activity`,
        signal,
      ),
    enabled: !!id && sharing,
    staleTime: 60_000,
  });
}

/** What a history reset is allowed to forget. */
export type HistoryScope = "all" | "finished" | "unfinished";

/*
 * How many playback records a reset would remove, without removing them.
 *
 * Its own query rather than a number computed on the client, because the client
 * does not hold the history — it holds a page of it. Asking the server is the
 * only way the confirmation can be *true*, and a confirmation that names a
 * number it guessed is worse than one that names none.
 */
export function useHistoryCount(scope: HistoryScope, enabled = true) {
  return useQuery({
    queryKey: ["history-count", scope],
    queryFn: ({ signal }) =>
      apiGet<{ count: number; scope: string }>(
        `/api/profile/history?scope=${scope}`,
        signal,
      ),
    enabled,
    // Refetched whenever the panel is opened rather than served from cache: a
    // number that is quietly an hour old is exactly the kind of stale figure
    // somebody would confirm an irreversible action against.
    staleTime: 0,
  });
}

/*
 * Forgetting it.
 *
 * Invalidates far more than it writes, and deliberately. This is the mutation
 * that changes what nearly every list in the app is showing — Continue
 * Watching, the unwatched filter, every grid's progress bars, the profile's
 * own totals. The rule this project keeps relearning is to ask what a person
 * could be *looking at* that a write changes, rather than what it writes.
 */
export function useResetHistory() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { scope: HistoryScope }) =>
      apiSend(`/api/profile/history?scope=${v.scope}`, "DELETE"),
    onSuccess: () => {
      /*
       * `["items"]` reaches the browse grid's `["items", "infinite", …]` by
       * prefix — that is the distinction this project has shipped the wrong
       * side of before, and the reason the grid's key is a *child* of the one
       * callers invalidate rather than a sibling like `["items-infinite"]`.
       */
      for (const key of [
        "history-count",
        "profile",
        "items",
        "continue",
        "recently-added",
        "review",
        "children",
      ]) {
        qc.invalidateQueries({ queryKey: [key] });
      }
    },
  });
}

export function useSetSharing() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (share: boolean) =>
      apiSend("/api/profile/sharing", "PUT", { share }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["people"] });
      qc.invalidateQueries({ queryKey: ["auth-status"] });
    },
  });
}

/*
 * Presence on paired servers (ADR 0045).
 *
 * Polled rather than cached, because presence is a claim about *now* and a
 * stale one is not a late answer but a wrong one. Ten seconds is chosen against
 * the server's own twenty-second watching timeout: long enough not to hammer
 * two servers over a WAN, short enough that a film starting or stopping shows
 * up before somebody wonders whether the page is broken.
 *
 * The key is `peer-presence` and not a child of `["people"]`. That is the
 * project's most-repeated bug in its most avoidable form: a sibling key gets
 * swept by every prefix invalidation aimed at the other thing, and the two
 * lists answer different questions from different servers.
 */
export function usePeerPresence() {
  return useQuery({
    queryKey: ["peer-presence"],
    queryFn: ({ signal }) =>
      apiGet<{ peers: PeerPresence[] }>("/api/people/peers", signal),
    refetchInterval: 10_000,
    staleTime: 5_000,
  });
}

/*
 * Granting is a decision about yourself, so the switch answers immediately.
 *
 * **Optimistically, and that is not a polish detail.** The write itself takes
 * about ten milliseconds — it sets one local row. But the list it changes is
 * built by calling every paired server, which costs two seconds when they
 * answer and six when one of them does not. Invalidating and waiting therefore
 * put a round trip to *somebody else's machine* between a person and their own
 * consent, and the tick sat unmoved for long enough to be clicked again.
 *
 * A grant is local truth the moment it is written, so the cache is corrected
 * here and the refetch happens behind it. `onError` puts the previous value
 * back: the one thing worse than a slow switch is one that shows a permission
 * that was never granted.
 *
 * Both keys, deliberately. A person reading Settings → Account wants the count
 * of who they share with to agree with what they just clicked — a write that
 * changes what a list holds must invalidate that list, and the question is what
 * somebody could be *looking at*, not what the write touched.
 */
export function useGrantPresence() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { fingerprint: string; person: string; on: boolean }) =>
      apiSend(
        `/api/people/peers/${encodeURIComponent(v.fingerprint)}/${encodeURIComponent(v.person)}/presence`,
        "PUT",
        { on: v.on },
      ),
    onMutate: async (v) => {
      // Stop an in-flight read from landing after this and undoing it.
      await qc.cancelQueries({ queryKey: ["peer-presence"] });
      const previous = qc.getQueryData<{ peers: PeerPresence[] }>([
        "peer-presence",
      ]);
      if (previous) {
        qc.setQueryData<{ peers: PeerPresence[] }>(["peer-presence"], {
          peers: previous.peers.map((peer) =>
            peer.fingerprint !== v.fingerprint
              ? peer
              : {
                  ...peer,
                  people: peer.people.map((person) =>
                    person.id === v.person
                      ? { ...person, granted: v.on }
                      : person,
                  ),
                },
          ),
        });
      }
      return { previous };
    },
    onError: (_err, _v, ctx) => {
      if (ctx?.previous) qc.setQueryData(["peer-presence"], ctx.previous);
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: ["peer-presence"] });
      void qc.invalidateQueries({ queryKey: ["profile"] });
    },
  });
}
