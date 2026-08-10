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

// A picture library's top level is galleries (ADR 0028). Sorted by when the
// picture was taken rather than by title, because the titles are filenames and
// the filenames are UUIDs — "Title" is offered last and mostly for completeness.
// Year and rating are absent: neither exists for a photograph here.
const PICTURE: LibraryKindConfig = {
  searchPlaceholder: "Search galleries",
  sorts: [
    { value: "taken", label: "Date taken" },
    { value: "added", label: "Recently added" },
    { value: "title", label: "Title" },
  ],
};

export function configForKind(kind: string | undefined): LibraryKindConfig {
  switch (kind) {
    case "show":
      return SHOW;
    case "music":
      return MUSIC;
    case "picture":
      return PICTURE;
    default:
      return MOVIE;
  }
}

// The library kinds a person can choose from, in one place because two screens
// need the same list and the same words: the add-library form offers them, and
// a scan result names the one that ignored your files. Two lists that drift
// would have Settings say "Movies" where the form said "Film".
//
// The set is open server-side (ADR 0018) — these are the ones a client offers.
export const LIBRARY_KINDS: { value: string; label: string }[] = [
  { value: "movie", label: "Movies" },
  { value: "show", label: "Shows" },
  { value: "music", label: "Music" },
  { value: "picture", label: "Pictures" },
  { value: "other", label: "Other" },
];

// kindLabel names a kind for display. An unfamiliar kind returns itself rather
// than an empty string, so a server that grows a kind this client predates
// still reads as something rather than as a gap.
export function kindLabel(kind: string | undefined): string {
  return LIBRARY_KINDS.find((k) => k.value === kind)?.label ?? kind ?? "unset";
}
