package scan

import (
	"fmt"

	"lancast/internal/store"
)

/*
 * The post-scan sanity check: does what came out look like what was asked for?
 *
 * Deliberately not another skip count. A skip count answers "what did I refuse
 * to import", which is the right question for a music library created as a
 * movie library — its audio is discarded outright and the count explains an
 * empty library. It is the wrong question for a shows library created as a
 * movie library, which skips nothing, imports everything, and is wrong in its
 * *shape*: every episode becomes a film sitting loose in the grid, and a
 * miniseries becomes one film in three parts.
 *
 * So this reads the census instead, and it is a *verdict* rather than a
 * measurement, because "1 movie, 3 parts, 0 shows" is not something a person
 * should have to interpret at the end of a scan.
 *
 * Pure, and takes the shape rather than a database, so every rule below is
 * tested against a struct literal in microseconds. The rules will be argued
 * with — thresholds always are — and an argument settled by editing a number
 * and re-running a table is a cheap argument.
 */

// ShapeWarning is a verdict about a finished scan. Empty Code means no warning.
type ShapeWarning struct {
	// Code is stable and machine-readable; the client chooses its own wording
	// for a code it knows and falls back to Message for one it does not.
	Code string `json:"code"`
	// Message is a whole sentence, written server-side, saying what happened
	// and what it means. The audit log takes the same position for the same
	// reason: a client that has to assemble prose from a code will assemble it
	// differently from the next client.
	Message string `json:"message"`
	// Remedy is separate from Message because it is the part that is hard to
	// hear: kind cannot be changed, so the only fix is to remove the library
	// and add it again. Saying that plainly beats implying a settings toggle
	// exists.
	Remedy string `json:"remedy,omitempty"`
}

/*
 * Thresholds, named rather than inlined.
 *
 * A shows library legitimately contains a few loose files — an extras folder, a
 * documentary shipped beside a series — so "any movie at all" would cry wolf on
 * ordinary libraries, and a check that cries wolf gets ignored, which is worse
 * than no check. These are set where the *shape* is wrong rather than merely
 * mixed.
 */
const (
	// A movie library where this share of items parsed as episodes is not a
	// movie library with a few oddities in it.
	episodeShareInMovieLibrary = 0.4
	// Below this many items, proportions mean nothing: two files, both
	// episodes, is a folder somebody is still filling.
	minimumItemsToJudge = 5
)

// CheckShape returns a warning when a finished scan's output does not look like
// the kind it was scanned as. An empty Code means the library looks right.
func CheckShape(kind string, sh store.LibraryShape, p Progress) ShapeWarning {
	switch kind {
	case "show":
		// A shows library that produced no shows produced no series structure
		// at all — every file landed as something else. This is the one case
		// with no threshold, because zero is not a proportion: a shows library
		// with even one show in it is doing what it was made for.
		if sh.Total >= minimumItemsToJudge && sh.Shows == 0 {
			return ShapeWarning{
				Code: "no_shows_in_show_library",
				Message: fmt.Sprintf(
					"This library was created for TV shows, but the scan produced no shows at all — %d items imported and not one series among them.",
					sh.Total),
				Remedy: "Check the folder layout: episodes are matched from names like Series/Season 01/S01E02. If this is a film library, remove it and add it again as Movies — a library's kind cannot be changed after it is created.",
			}
		}

	case "movie":
		// The other direction, and the one that produces a library which looks
		// finished and is wrong. EpisodesInMovieLibrary is counted during the
		// walk, from the filename parse, so it sees the files that *read* as
		// episodes even where the importer then made something else of them.
		if p.EpisodesInMovieLibrary > 0 && sh.Total >= minimumItemsToJudge {
			share := float64(p.EpisodesInMovieLibrary) / float64(sh.Total)
			if share >= episodeShareInMovieLibrary {
				return ShapeWarning{
					Code: "episodes_in_movie_library",
					Message: fmt.Sprintf(
						"This library was created for films, but %d of %d files are named like TV episodes. They have been imported as films, and a series spread across several files may have become one film in several parts.",
						p.EpisodesInMovieLibrary, sh.Total),
					Remedy: "If this is a TV library, remove it and add it again as TV Shows — a library's kind cannot be changed after it is created.",
				}
			}
		}
	}

	// The audio-versus-video case keeps its existing signal rather than gaining
	// a second one. SkippedKind already explains an empty library, and two
	// warnings about one mistake is one warning too many — but a library that
	// imported *nothing* while discarding a great deal is worth saying out
	// loud, because "0 items · scanned" reads as success.
	if sh.Total == 0 && p.SkippedKind > 0 {
		return ShapeWarning{
			Code: "everything_skipped_for_kind",
			Message: fmt.Sprintf(
				"Nothing was imported: all %d media files found are of a type this library's kind does not accept.",
				p.SkippedKind),
			Remedy: "A music library scans audio and a film or TV library scans video. Remove this library and add it again with the kind that matches its contents.",
		}
	}

	return ShapeWarning{}
}
