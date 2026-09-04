// Shapes returned by the LANcast HTTP API.
//
// Types that docs/openapi.json specifies are re-exported from the generated
// schema.ts rather than restated here, so there is exactly one description of
// each shape and the client cannot disagree with the contract. The spec is
// checked against the router by internal/api/openapi_test.go, and schema.ts is
// checked against the spec by schema.test.ts — so a shape that reaches this
// file has been verified end to end.
//
// The rest are still hand-written, and are migrated a section at a time as the
// spec grows. `pendingSpec` in internal/api/openapi_test.go is the list of what
// has not been reached yet.

import type { components } from "./schema";

export type MatchState =
  "matched" | "review" | "unmatched" | "locked" | "local";

/** One place a library's files live (ADR 0034). */
export type LibraryRoot = components["schemas"]["LibraryRoot"];

export type Library = components["schemas"]["Library"];

export type Artwork = components["schemas"]["Artwork"];

export type Progress = components["schemas"]["Progress"];

export type Credit = components["schemas"]["Credit"];

/*
 * One external score (ADR 0019). Documented on the generated type.
 *
 * Named ExternalRating rather than Rating because it was `Rating`, and so is
 * the caller's *own* rating further down this file. Two interfaces of the same
 * name in one module do not collide — TypeScript merges them — so the two
 * became a single type requiring `source`, `display`, `item_id` and
 * `updated_at` all at once, and neither endpoint ever sends that. Nothing
 * failed, because a response is cast to the type rather than checked against
 * it.
 *
 * docs/api.md is explicit that these are distinct: "Three numbers about one
 * film is one too many to leave unlabelled, so they are never merged into a
 * single field." Two of them had been merged for as long as both existed.
 */
export type ExternalRating = components["schemas"]["ItemRating"];

export interface Trailer {
  site: string;
  key: string;
  name?: string;
}

export interface SubtitleCandidate {
  file_id: number;
  file_name: string;
  release: string;
  language: string;
  download_count: number;
  fps?: number;
  hash_match: boolean;
  hearing_impaired: boolean;
  forced: boolean;
  uploader?: string;
  score: number;
  reason?: string;
}

export interface SubtitleSearchResult {
  item_id: number;
  hash_used: boolean;
  candidates: SubtitleCandidate[];
  auto_match: boolean;
  auto_match_key: number;
}

export interface SubtitleTrack {
  key: string;
  label: string;
  language?: string;
  source: string; // embedded | sidecar | downloaded
  codec?: string;
  forced: boolean;
  default: boolean;
  available: boolean;
  reason?: string;
}

// One track inside a media file, as the probe found it. `index` is absolute
// within the file, which is what `?audio=` takes (docs/api.md).
export type MediaStream = components["schemas"]["MediaStream"];

export type Item = components["schemas"]["Item"];

// The anatomy of a candidate's score: sub-scores (0..1) that combine by their
// weights into the total. Nested keys are lowercase (the Go struct tags them).
export interface ScoreBreakdown {
  title: number;
  year: number;
  popularity: number;
  total: number;
  year_gap: number;
}

// A possible metadata match from a provider. Fields are PascalCase because the
// server serializes meta.Candidate without json tags.
export interface MatchCandidate {
  Provider: string;
  ExternalID: string;
  Kind: string;
  Title: string;
  Year: number;
  Overview: string;
  Popularity: number;
  PosterURL: string;
  Score: number;
  Breakdown: ScoreBreakdown;
}

export type ItemsPage = components["schemas"]["ItemsPage"];

// The filter values a library's browse view offers. Only values actually
// present are returned, so a chosen filter never empties the grid. has_watched
// gates the unwatched-only toggle: offered only when it would remove something.
export interface Facets {
  /** First letters present in this library, "#" first. Drives the A–Z rail. */
  initials?: string[];
  genres: string[];
  decades: number[];
  content_ratings: string[];
  has_watched: boolean;
  /** Exact years present, newest first. Searchable rather than chipped. */
  years: number[];
  /** Resolution tiers present, widest first. Labels come from the server so
   *  the client never invents a name for a bucket it did not define. */
  resolutions: ResolutionBucket[];
  has_in_progress: boolean;
  has_unmatched: boolean;
  /** Collections in this library, most-populated first. */
  collections: CollectionFacet[];
  /** The highest rating present, so no threshold is offered that cannot match. */
  max_rating: number;
}

export interface CollectionFacet {
  id: number;
  name: string;
  members: number;
}

export interface ResolutionBucket {
  key: string;
  label: string;
  min_width: number;
  max_width: number;
}

/** One credited person, with how much of the library they are in. */
export interface CastMember {
  id: number;
  name: string;
  role: string;
  items: number;
}

export interface Encoder {
  name: string;
  label: string;
  hardware: boolean;
}

export interface Settings {
  tmdb: { configured: boolean };
  opensubtitles: { configured: boolean };
  omdb: { configured: boolean };
  // Whether the server can inspect and convert media. Without these every file
  // is direct-played, and anything the browser cannot decode fails silently.
  media_tools: {
    probe_available: boolean;
    transcode_available: boolean;
    directory: string;
  };
  rate_per_sec: number;
  write_nfo: boolean;
  /* Whether picture folders and photos can be marked sensitive (ADR 0051). */
  sensitive_marking: boolean;
  detect_markers: boolean;
  auto_enrich: boolean;
  update_check: boolean;
  encoder: { preference: string; active: Encoder; available: Encoder[] };
  // Server rules: what a client shows and what it may do. They live on the
  // server because a household with a phone, a browser and a TV must not hold
  // three answers to "have I watched this".
  debug_logging: boolean;
  watched_threshold: number;
  continue_weeks: number;
  continue_limit: number;
  allow_media_deletion: boolean;
  // Optional because a server older than this field does not send it, and a
  // client newer than its server is ordinary here. Absent reads as off, which
  // is the safe direction for a switch that removes records.
  empty_trash_on_scan?: boolean;
  scan_interval_hours: number;
}

export interface SettingsUpdate {
  tmdb_key?: string;
  opensubtitles_key?: string;
  omdb_key?: string;
  ffmpeg_dir?: string;
  rate_per_sec?: number;
  write_nfo?: boolean;
  sensitive_marking?: boolean;
  detect_markers?: boolean;
  auto_enrich?: boolean;
  update_check?: boolean;
  hardware_encoder?: string;
  debug_logging?: boolean;
  watched_threshold?: number;
  continue_weeks?: number;
  continue_limit?: number;
  allow_media_deletion?: boolean;
  empty_trash_on_scan?: boolean;
  scan_interval_hours?: number;
}

export interface ScanIssue {
  path: string; // library-relative
  reason: string;
}

/**
 * A finished scan's verdict on its own output (GET /api/libraries/{id}/scan).
 *
 * `code` is stable and machine-readable so a client can choose its own
 * treatment for a case it knows; `message` is the fallback for one it does not,
 * and the set is open. `remedy` is separate because it is the hard part to
 * hear: a library's kind cannot be changed, so the only fix is to remove it and
 * add it again.
 */
export type ShapeWarning = components["schemas"]["ShapeWarning"];

/**
 * What scanning every library at once answered.
 *
 * Both halves matter to the caller. A sweep that started nothing because every
 * library was already scanning is indistinguishable from a sweep that did
 * nothing at all, unless `busy` is reported alongside `started`.
 */
export interface ScanAllResult {
  started: ScanStatus[];
  /** Library ids that were already scanning and were left to finish. */
  busy: number[];
}

export interface ScanStatus {
  library_id: number;
  state: string; // idle | running | complete | failed
  files_seen: number;
  items_changed: number;
  items_missing: number;
  skipped: number;
  // Media the library's kind excludes — audio in a video library, video in a
  // music library. Not a failure, which is why it is not part of `skipped`:
  // it answers "why is this library empty", not "what went wrong".
  skipped_kind: number;
  // Trailers, featurettes, deleted scenes and sample files left out of a video
  // library (ADR 0038). Not a failure and not part of `skipped`: nothing went
  // wrong reading them, they are simply not works.
  skipped_extras: number;
  // Tracks whose tags could not be read, taking their title and album from the
  // folder and filename instead. Not part of `skipped` and not a failure: the
  // file imported and plays. It has its own number because counting it as
  // skipped made a library report failures it could not name.
  skipped_untagged: number;
  /** Files that parsed as episodes in a library that says it holds films. */
  episodes_in_movie_library?: number;
  /**
   * The verdict those counts feed: present when a finished scan produced
   * something that does not look like the kind it was scanned as.
   *
   * The sentence is written server-side and rendered as given. The client used
   * to assemble its own from `episodes_in_movie_library`, which worked for the
   * one case it knew about and could say nothing about the other — a shows
   * library that produced no shows is not visible in any count the client
   * receives. Prose the client reconstructs is prose that drifts from the rule
   * that decided it.
   */
  shape_warning?: ShapeWarning;
  /** How many of the library's locations this scan actually read (ADR 0034). */
  roots_scanned?: number;
  /**
   * Locations the scan could not read — an unplugged drive, a disconnected
   * share. Not a failure when others were readable: the scan did real work on
   * the rest, and this is what stops it looking complete when it covered half
   * the library.
   */
  roots_skipped?: { id: number; path: string }[];
  issues?: ScanIssue[];
  started_at: number;
  finished_at?: number;
  error?: string;
}

export interface BrowseEntry {
  name: string;
  path: string;
}

export interface BrowseResult {
  path: string;
  parent: string | null;
  entries: BrowseEntry[];
}

export interface ApiError {
  error: { code: string; message: string };
}

// A plugin's capability set — the hosts it may reach and the secrets it may
// read (ADR 0021). Shown as "requested" (what the manifest asks) vs "granted"
// (what the operator approved).
export interface PluginCaps {
  http: string[];
  secrets: string[];
}

export type PluginSigner = "first_party" | "pinned" | "unsigned";

export interface Plugin {
  name: string;
  version: string;
  kind: string;
  signer: PluginSigner;
  enabled: boolean;
  digest: string;
  requested: PluginCaps;
  granted: PluginCaps;
  installed_at?: number;
}

export type Role = "admin" | "member";

export interface AuthUser {
  id: string;
  name: string;
  role: Role;
  /** This account's own ADR 0035 sharing choice. Absent only if the server
   *  could not read it — `/api/people` cannot answer this, because it excludes
   *  the caller. */
  sharing?: boolean;
  /** Whether this account appears in the roster handed to paired servers
   *  (ADR 0044). Reported here for the same reason `sharing` is: nothing else
   *  can tell a client its own setting. */
  visible_to_peers?: boolean;
}

// GET /api/auth/status. `user` is present only when a session is active.
export interface AuthStatus {
  configured: boolean;
  authenticated: boolean;
  lan_enabled: boolean;
  // Whether restarting would let other devices reach the server. False when a
  // loopback address was configured deliberately — there a restart changes
  // nothing, so promising otherwise sends the operator on a dead end.
  restart_required: boolean;
  /*
   * Whether the server can convert files at all — false when ffmpeg is absent.
   *
   * Optional because a client may be newer than its server. Absent means "this
   * server does not say", which must read as *capable*: assuming a server
   * cannot convert would put a warning in front of somebody whose playback
   * works perfectly.
   */
  can_convert?: boolean;
  user?: AuthUser;
}

// Background probing progress. `available` is false when ffprobe is not
// installed, which is a supported configuration rather than an error.
export interface ProbeStatus {
  available: boolean;
  running: boolean;
  probed: number;
  failed: number;
  remaining: number;
  total: number;
}

// What a re-probe queued. `scope` echoes back which one ran, since the default
// is the narrow one and the caller should be able to tell them apart.
export interface ReprobeResult {
  scope: "incomplete" | "all";
  queued: number;
}

/*
 * What a re-parse did. Both numbers are needed to say anything useful: a run
 * that examined 160 rows and changed 98 repaired a library, and one that
 * examined 0 found nothing left to do. Reporting only `changed` makes those two
 * outcomes read identically as "0".
 */
export interface ReparseResult {
  examined: number;
  changed: number;
}

// One thing the server is doing right now (GET /api/activity). Every worker —
// scan, enrich, probe, coverart, transcode — reports this one shape, so the
// activity panel renders a list rather than five special cases.
export interface Activity {
  kind: "scan" | "enrich" | "probe" | "coverart" | "transcode" | "update";
  id: string;
  title: string;
  // "available" is the odd one: it is not work in progress but something the
  // server is waiting for the reader to do, and it renders as an action rather
  // than a progress bar.
  state: "running" | "failed" | "available";
  done: number;
  // 0 means indeterminate: a scan knows what it has seen, never what is left.
  total: number;
  detail?: string;
  library_id?: number;
  started_at?: number;
  error?: string;
}

export interface ActivityStatus {
  active: boolean;
  tasks: Activity[];
  /**
   * When background work last finished, in unix seconds. 0 means nothing has
   * ever finished, which is not the same as "just now".
   *
   * Watched instead of the active-to-idle edge, because work shorter than the
   * idle poll interval is never seen as active and produces no edge at all.
   */
  completed_at?: number;
  /**
   * The version waiting to be applied on the next restart, when there is one.
   *
   * Here rather than only on /api/update because the shell's stale-client
   * banner needs it and is shown to everyone, where /api/update is admin-only.
   * Absent means nothing is staged — which is the case where telling somebody
   * to restart is advice that cannot work.
   */
  staged?: string;
}

// GET /api/logs. `complete` is false when older lines exist that this response
// does not carry — the difference between "this is the log" and "this is the
// end of the log".
export interface ServerLog {
  lines: string[];
  complete: boolean;
  path: string;
}

// GET /api/audit — one recorded act (ADR 0026). `summary` and `actor_name` are
// resolved server-side at write time, so an event stays readable after the
// account and the row it names are both gone. The client renders them, never
// reconstructs them.
export interface AuditEvent {
  id: number;
  at: number;
  actor_id: string;
  actor_name: string;
  action: string;
  target_kind?: string;
  target_id?: string;
  summary: string;
  detail?: string;
}

export interface AuditPage {
  events: AuditEvent[];
  total: number;
  // The distinct actions actually present, so the filter is built from what
  // happened rather than from a list that drifts from the server.
  actions: string[];
}

// GET /api/update. `can_verify` is whether this build can check a release's
// signature at all — false means automatic installation is unavailable no
// matter what the setting says, and the UI must say so rather than offering a
// button that cannot work.
export interface UpdateStatus {
  supported: boolean;
  current?: string;
  latest?: string;
  available?: boolean;
  url?: string;
  checked_at?: number;
  checking?: boolean;
  error?: string;
  // The last failed download, distinct from a failed check: "I could not ask"
  // versus "I asked, and installing it failed". A download runs detached from
  // the request that starts it, so this is the only way its outcome is visible.
  download_error?: string;
  can_verify?: boolean;
  enabled?: boolean;
  // Set once an update is downloaded and verified. Distinct from `available`:
  // available means decide, staged means restart.
  staged?: string;
  staged_at?: number;
  downloading?: {
    active: boolean;
    done: number;
    total: number;
    stage?: string;
  };
}

// GET /api/profile. Identity, totals and history in one response — a page that
// needs all three should not discover that from three round trips.
export interface ProfileUser {
  id: string;
  name: string;
  admin: boolean;
  // False on an unconfigured loopback server, where there is no account and the
  // history belongs to the migrated 'local' id. The page says so rather than
  // inventing a person.
  secured: boolean;
}

export interface ProfileStats {
  started: number;
  // Two different questions, and keeping them apart is the point: `finished` is
  // how many distinct titles have been finished, `viewings` is how many times
  // finishing happened. Somebody who has seen twelve films, one of them nine
  // times, has finished twelve things and sat through twenty.
  finished: number;
  // Optional because a server older than this field simply does not send it,
  // and a client newer than its server is ordinary here.
  viewings?: number;
  // Time spent, not runtime owned: an unfinished item counts how far in you
  // got, so eleven abandoned films are not eleven hours — and a rewatched one
  // counts its runtime per viewing, so nine sittings are not one.
  watched_ms: number;
  first_at: number | null;
}

export interface HistoryEntry {
  item: Item;
  position_ms: number;
  watched: boolean;
  played_at: number;
}

export interface Profile {
  user: ProfileUser;
  stats: ProfileStats;
  history: HistoryEntry[];
  has_more: boolean;
}

// GET /api/crashes — a recovered panic. `where` is the route pattern rather
// than the URL: the pattern is what somebody fixes.
export interface CrashReport {
  id: string;
  at: number;
  kind: string;
  where: string;
  value: string;
  stack: string;
  version: string;
}

// GET /api/libraries/{id}/trending. `viewers` counts *accounts*, not plays:
// playback_state holds one row per item per user, so this is how many people
// played something recently rather than how often it was played.
export interface TrendingItem {
  item: Item;
  viewers: number;
  finishers: number;
  last_at: number;
}

export interface Trending {
  items: TrendingItem[];
  // How many accounts contributed anything in the window. With one, this is
  // honestly "recently played" and not a trend — the client is given the number
  // so it can say the true thing rather than calling it trending regardless.
  contributors: number;
  window_days: number;
}

// GET /api/items/{id}/rating — *your* rating. There is no route to anybody
// else's: a rating is private to the account that wrote it, and the paths carry
// no user id so a filter cannot be forgotten.
export interface Rating {
  item_id: number;
  /** 1–10, not 1–5: a half-star interface then needs no migration. */
  score: number;
  review?: string | null;
  updated_at: number;
}

export interface RatedItem {
  item: Item;
  rating: Rating;
}

// GET /api/together — a watch-together room. The server owns position and
// paused; clients converge on them rather than each broadcasting their own.
export interface TogetherMember {
  user_id: string;
  name: string;
  host: boolean;
  last_seen: number;
}

export interface TogetherSession {
  id: string;
  item_id: number;
  host_id: string;
  position_ms: number;
  paused: boolean;
  /** When the host last reported, so a follower can allow for the time since. */
  updated_at: number;
  members: TogetherMember[];
  created_at: number;
}

// PATCH /api/users/{id} — an account as the manager sees it. `sessions` is live
// sessions, not a login history: it answers "is this person here right now".
export interface ManagedUser {
  id: string;
  name: string;
  role: Role;
  created_at: number;
  sessions: number;
}

// GET /api/channels — Live TV. A channel is deliberately not an Item: it has no
// duration, no file and no identity a provider could match. The upstream URL is
// never sent to clients, because channel lists are routinely credentialed.
export interface Channel {
  id: number;
  source_id: number;
  name: string;
  logo_url: string | null;
  group: string | null;
  position: number;
  // The XMLTV id listings arrive under. Null means this channel can never have
  // a guide — the playlist did not say which channel it is, and matching by
  // name would attach "BBC One" listings to "BBC One HD".
  tvg_id: string | null;
}

export interface ChannelSource {
  id: number;
  name: string;
  url: string;
  created_at: number;
  refreshed_at: number | null;
  channel_count: number;
  epg_url: string | null;
  epg_refreshed_at: number | null;
  program_count: number;
}

// GET /api/guide, GET /api/channels/{id}/guide — one entry in the schedule.
// Times are unix seconds; a programme is an interval, not a duration, because
// live television has no "position" to resume from.
export interface Program {
  id: number;
  channel_id: number;
  start_at: number;
  stop_at: number;
  title: string;
  description: string | null;
  category: string | null;
  season: number | null;
  episode: number | null;
  icon_url: string | null;
}

// GET /api/guide — keyed by channel id (as a string, because JSON keys are).
// A channel with no listings is absent rather than present and empty, so "no
// guide" can be told from "nothing on".
export interface GuideNow {
  now: Program;
  next?: Program;
}

// GET /api/people — the other accounts on this server (ADR 0035). `sharing` is
// reported even when false, so a page can say "has not shared" rather than
// showing an empty list that reads as "watches nothing".
export interface Person {
  id: string;
  name: string;
  role: Role;
  sharing: boolean;
  watched: number;
  joined_at: number;
}

/** A pinned media-tools build, as offered before it is downloaded. A download
 *  the user cannot identify is not consent, so this is shown, not implied. */
export interface MediaToolsSource {
  version: string;
  licence: string;
  licence_url: string;
  size_bytes: number;
  url: string;
}

/** The state of fetching ffmpeg (ADR 0043). */
export interface MediaToolsState {
  running: boolean;
  stage: "" | "downloading" | "verifying" | "installing";
  bytes_done: number;
  bytes_total: number;
  error?: string;
  finished_at?: number;
  probe_available: boolean;
  transcode_available: boolean;
  directory?: string;
  /** Absent where there is no pinned build for the platform. */
  available_source?: MediaToolsSource;
}

/*
 * Somebody on a paired server, and what ADR 0045 §3 permits to be said of them.
 *
 * `shares` is not `online` and not `watching`. It is whether *they* have granted
 * *you* presence at all, and it is separate because "has not shared with you",
 * "offline" and "online and idle" are three different statements. The People
 * page is required to tell them apart, which it can only do if the type keeps
 * them apart first — collapsing them into one optional string is how a choice
 * gets rendered as an absence.
 */
export interface PeerPerson {
  id: string;
  name: string;
  /** Whether you have granted them your presence. Your decision, not theirs. */
  granted: boolean;
  /** Whether they have granted you theirs. */
  shares: boolean;
  online?: boolean;
  /** The work, by title. Never an episode, never a position. */
  watching?: string;
}

export interface PeerPresence {
  fingerprint: string;
  name: string;
  state: string;
  /** Whether this server answered just now. Distinct from anybody being online. */
  reachable: boolean;
  people: PeerPerson[];
}

/*
 * A work claimed by more than one file (ADR 0042).
 *
 * LANcast reports these and resolves none of them. A shared provider id is
 * evidence that something wants a human, not that anything is duplicated: of
 * the thirteen pairs in the library this was built against, two were not
 * duplicates at all — a film split across two discs, and a 1989 film wearing a
 * 2022 film's identity from a stale .nfo.
 */
export type CollisionMember = {
  id: number;
  title: string;
  /** The one place this API returns a path: the report is for going and
   *  looking at the two files. Admin-only for that reason. */
  path: string;
  /** What the filename claimed. A label, never a grouping key — the file that
   *  motivated ADR 0042 called itself an alternate cut and was a copy. */
  edition?: string;
  size_bytes: number | null;
  library_id: number;
  missing: boolean;
  /** Present only after a comparison. Sampled, not exhaustive. */
  fingerprint?: string;
  /** Could not be read. An absence of evidence, not a difference. */
  unreadable?: boolean;
};

export type Collision = {
  provider: string;
  external_id: string;
  same_size: boolean;
  members: CollisionMember[];
  /** Absent until compared, and absent afterwards if any member was
   *  unreadable. "Identical so far as sampled" — never "identical". */
  same_bytes?: boolean;
  /** When somebody looked at exactly these rows and accepted them. Absent on a
   *  server too old to record it, which reads the same as never dismissed. */
  dismissed_at?: number;
};

/*
 * Backups (ADR 0058). A backup is the database and nothing else: no artwork,
 * which is re-fetchable, and no sessions, which are cleared when it is written.
 */
export interface BackupFile {
  name: string;
  bytes: number;
  taken_at: number;
  schema_version: number;
  // Whether *this build* could restore it. Migrations are one-way, so a backup
  // from a newer LANcast cannot be, and `problem` says so in a sentence.
  restorable: boolean;
  problem?: string;
}

export interface BackupsResponse {
  backups: BackupFile[];
  // Where the files are, shown so somebody can copy one off this disk.
  folder: string;
  // What to type to restore. The client cannot perform one — restoring
  // replaces the database the server is reading — so it shows the command
  // rather than a button that would have to lie.
  restore_command: string;
}
