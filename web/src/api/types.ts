// Shapes returned by the LANcast HTTP API. Kept deliberately close to the
// server's JSON — the API contract in docs/api.md is the source of truth.

export type MatchState =
  | "matched"
  | "review"
  | "unmatched"
  | "locked"
  | "local";

export interface Library {
  id: number;
  name: string;
  kind: string;
  path: string;
  created_at: number;
  scanned_at: number | null;
  item_count: number;
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
