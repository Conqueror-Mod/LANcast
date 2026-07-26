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

export interface Item {
  id: number;
  library_id: number;
  kind: string;
  title: string;
  year: number | null;
  series: string | null;
  season: number | null;
  episode: number | null;
  duration_ms: number | null;
  added_at: number;
  missing: boolean;
  parent_id: number | null;
  overview?: string | null;
  rating?: number | null;
  content_rating?: string | null;
  released_at?: number | null;
  genres?: string[];
  credits?: Credit[];
  match_state?: MatchState;
  metadata_updated_at?: number | null;
  artwork?: Artwork;
  progress?: Progress | null;
}

export interface ItemsPage {
  items: Item[];
  total: number;
}

export interface Encoder {
  name: string;
  label: string;
  hardware: boolean;
}

export interface Settings {
  tmdb: { configured: boolean };
  opensubtitles: { configured: boolean };
  rate_per_sec: number;
  write_nfo: boolean;
  auto_enrich: boolean;
  encoder: { preference: string; active: Encoder; available: Encoder[] };
}

export interface SettingsUpdate {
  tmdb_key?: string;
  opensubtitles_key?: string;
  rate_per_sec?: number;
  write_nfo?: boolean;
  auto_enrich?: boolean;
  hardware_encoder?: string;
}

export interface ScanStatus {
  library_id: number;
  state: string; // idle | running | complete | failed
  files_seen: number;
  items_changed: number;
  items_missing: number;
  started_at: number;
  finished_at?: number;
  error?: string;
}

export interface ApiError {
  error: { code: string; message: string };
}
