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

// Sort values the API supports (title | year | added | rating).
const MOVIE: LibraryKindConfig = {
  searchPlaceholder: "Search this library",
  sorts: [
    { value: "title", label: "Title" },
    { value: "year", label: "Year" },
    { value: "added", label: "Recently added" },
    { value: "rating", label: "Rating" },
  ],
};

const SHOW: LibraryKindConfig = {
  searchPlaceholder: "Search shows",
  sorts: [
    { value: "title", label: "Title" },
    { value: "year", label: "First aired" },
    { value: "added", label: "Recently added" },
    { value: "rating", label: "Rating" },
  ],
};

// A music library's top level is artists (ADR 0024). Rating is deliberately
// absent: there is no music rating source — ADR 0024 defers MusicBrainz and
// every rating LANcast holds comes from TMDB or OMDb — so offering the sort
// would promise an ordering the data cannot produce.
//
// The facet row needs no configuration. Facets return only values actually
// present, so a tag-only music library yields no genres and no content ratings
// and those chips simply do not render.
const MUSIC: LibraryKindConfig = {
  searchPlaceholder: "Search artists",
  sorts: [
    { value: "title", label: "Title" },
    { value: "year", label: "Year" },
    { value: "added", label: "Recently added" },
  ],
};

export function configForKind(kind: string | undefined): LibraryKindConfig {
  switch (kind) {
    case "show":
      return SHOW;
    case "music":
      return MUSIC;
    default:
      return MOVIE;
  }
}
