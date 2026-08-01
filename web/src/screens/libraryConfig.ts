// Per-library-kind browse configuration. A movie library and a TV library share
// the LibraryView shell but differ in their sort options and copy. The set of
// kinds is open (ADR 0018), so an unrecognised kind falls back to the movie
// config rather than rendering nothing.

export interface SortOption {
  value: string; // maps to /api/items ?sort= — must be a supported value
  label: string;
}

export interface LibraryKindConfig {
  searchPlaceholder: string;
  sorts: SortOption[];
}

// Only sort values the API supports today (title | year | added). Richer sorts
// (rating, recently-aired) arrive with the Phase 2/3 API work.
const MOVIE: LibraryKindConfig = {
  searchPlaceholder: "Search this library",
  sorts: [
    { value: "title", label: "Title" },
    { value: "year", label: "Year" },
    { value: "added", label: "Recently added" },
  ],
};

const SHOW: LibraryKindConfig = {
  searchPlaceholder: "Search shows",
  sorts: [
    { value: "title", label: "Title" },
    { value: "year", label: "First aired" },
    { value: "added", label: "Recently added" },
  ],
};

export function configForKind(kind: string | undefined): LibraryKindConfig {
  return kind === "show" ? SHOW : MOVIE;
}
