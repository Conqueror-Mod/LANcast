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
  match_state?: MatchState;
  metadata_updated_at?: number | null;
  artwork?: Artwork;
  progress?: Progress | null;
}

export interface ItemsPage {
  items: Item[];
  total: number;
}

export interface ApiError {
  error: { code: string; message: string };
}
