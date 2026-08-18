// Shapes returned by the LANcast HTTP API. Kept deliberately close to the
// server's JSON — the API contract in docs/api.md is the source of truth.

export type MatchState =
  | "matched"
  | "review"
  | "unmatched"
  | "locked"
  | "local";

/** One place a library's files live (ADR 0034). */
export interface LibraryRoot {
  id: number;
  library_id: number;
  path: string;
  created_at: number;
  /** Rows that would go with this location if it were removed. */
  item_count: number;
}

export interface Library {
  id: number;
  name: string;
  kind: string;
  /**
   * The library's first location.
   *
   * Kept by the server so clients that predate multi-root libraries keep
   * working, and superseded by `roots`. Read `roots` for anything that has to
   * be right about a library in more than one place; this stays correct for
   * the single-location case, which is most of them.
   */
  path: string;
  roots?: LibraryRoot[];
  created_at: number;
  scanned_at: number | null;
  item_count: number;
  /**
   * Files in the library — songs, photos, films, episodes — as against
   * `item_count`, which is tiles in the grid. They differ wherever a library
   * groups its media: 1,171 artists holding tens of thousands of songs.
   */
  media_count: number;
  /**
   * The last scan's verdict, when it had one. Carried on the library rather
   * than only in live scan progress, which dies with the server process — a
   * warning about a kind that cannot be changed needs to outlive a restart.
   */
  shape_warning?: ShapeWarning;
}

export interface Artwork {
  poster?: string;
  fanart?: string;
}

export interface Progress {
  position_ms: number;
  watched: boolean;
}

export interface Credit {
  name: string;
  role: string;
  character?: string;
}

// One external score (ADR 0019). `source` is an open set — imdb, rotten_tomatoes,
// metacritic, and more later — so the client renders whatever arrives rather
// than switching on a fixed list. `score` is normalized 0–10; `display` is the
// source-native string ("88%", "81", "8.0").
export interface Rating {
  source: string;
  score: number;
  display: string;
  votes?: number;
}

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
export interface MediaStream {
  index: number;
  kind: string; // video | audio | subtitle
  codec: string;
  profile?: string;
  language?: string;
  title?: string;
  default: boolean;
  forced: boolean;
  channels?: number;
}

export interface Item {
  id: number;
  library_id: number;
  kind: string;
  title: string;
  year: number | null;
  // On a track these three are read in the music sense (ADR 0024): `series` is
  // the album, `season` the disc, `episode` the track number.
  series: string | null;
  season: number | null;
  episode: number | null;
  // The track's own performer, present only on music. Distinct from the album
  // artist that groups the record — on a compilation they differ, which is the
  // whole reason both exist.
  artist?: string | null;
  duration_ms: number | null;
  // The file's container and size. Both have always been in the item JSON and
  // no client had asked for either until the download button needed to propose
  // a filename and the downloads list needed to say how big the file was.
  container?: string | null;
  size_bytes?: number | null;
  added_at: number;
  missing: boolean;
  parent_id: number | null;
  child_count?: number;
  overview?: string | null;
  rating?: number | null;
  content_rating?: string | null;
  released_at?: number | null;
  genres?: string[];
  credits?: Credit[];
  provider?: string | null;
  external_id?: string | null;
  match_state?: MatchState;
  match_score?: number | null;
  metadata_updated_at?: number | null;
  file_name?: string;
  locked_fields?: string[] | null;
  ratings?: Rating[];
  artwork?: Artwork;
  // Present on a detail response: the full track list, including alternate
  // audio. Absent from list responses, which is why the player reads it from
  // the item it fetched rather than from the grid row that opened it.
  streams?: MediaStream[];
  // Pictures (ADR 0028). width/height describe the photo as it will be seen —
  // a quarter-turned phone photo reports its rotated dimensions, so a layout
  // can reserve the right box before the image loads. taken_at is EXIF capture
  // time, absent when the file carries none.
  width?: number | null;
  height?: number | null;
  taken_at?: number | null;
  progress?: Progress | null;
}

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

export interface ItemsPage {
  items: Item[];
  total: number;
}

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
  scan_interval_hours: number;
}

export interface SettingsUpdate {
  tmdb_key?: string;
  opensubtitles_key?: string;
  omdb_key?: string;
  ffmpeg_dir?: string;
  rate_per_sec?: number;
  write_nfo?: boolean;
  auto_enrich?: boolean;
  update_check?: boolean;
  hardware_encoder?: string;
  debug_logging?: boolean;
  watched_threshold?: number;
  continue_weeks?: number;
  continue_limit?: number;
  allow_media_deletion?: boolean;
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
export interface ShapeWarning {
  code: string;
  message: string;
  remedy?: string;
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
  downloading?: { active: boolean; done: number; total: number; stage?: string };
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
  finished: number;
  // Time spent, not runtime owned: an unfinished item counts how far in you
  // got, so eleven abandoned films are not eleven hours.
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
