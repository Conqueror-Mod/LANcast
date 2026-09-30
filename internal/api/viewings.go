package api

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"lancast/internal/store"
)

/*
 * The watch history, and a way to take it elsewhere (ADR 0074).
 *
 * Both answer about the caller and nobody else: the account comes from the
 * session, and there is no route naming another account. An export is a copy
 * of your own records handed to you; LANcast sends it nowhere, so "no
 * phone-home" holds — importing it into another service is something a person
 * does, not something the server does.
 */

// GET /api/profile/viewings — one page of finished viewings, newest first.
func (s *Server) listViewings(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 500 {
		limit = v
	}
	offset := 0
	if v, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && v > 0 {
		offset = v
	}
	vs, total, err := s.st.Viewings(r.Context(), s.userID(r), limit, offset)
	if err != nil {
		s.writeInternal(w, err, "viewings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"viewings": vs, "total": total})
}

// GET /api/profile/viewings/export?format=csv|trakt — the whole history as a
// file to save.
func (s *Server) exportViewings(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "csv"
	}
	if format != "csv" && format != "trakt" {
		writeError(w, http.StatusBadRequest, "bad_request", "format must be csv or trakt")
		return
	}
	vs, err := s.st.AllViewings(r.Context(), s.userID(r))
	if err != nil {
		s.writeInternal(w, err, "export viewings")
		return
	}
	stamp := time.Now().UTC().Format("2006-01-02")
	// Never cached: it is one person's history, and a shared cache holding it
	// would be the leak the rest of this file is careful to avoid.
	w.Header().Set("Cache-Control", "no-store")
	if format == "trakt" {
		w.Header().Set("Content-Disposition",
			fmt.Sprintf(`attachment; filename="lancast-history-%s-trakt.json"`, stamp))
		writeJSON(w, http.StatusOK, traktHistory(vs))
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="lancast-history-%s.csv"`, stamp))
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"watched_at", "kind", "title", "year", "series",
		"season", "episode", "imdb_id", "show_imdb_id", "estimated"})
	for _, v := range vs {
		_ = cw.Write([]string{
			watchedAt(v.FinishedAt), v.Kind, v.Title, intOr(v.Year), strOr(v.Series),
			intOr(v.Season), intOr(v.Episode), strOr(v.IMDbID), strOr(v.ShowIMDbID),
			strconv.FormatBool(v.Estimated),
		})
	}
	cw.Flush()
}

func watchedAt(unix int64) string {
	return time.Unix(unix, 0).UTC().Format("2006-01-02T15:04:05.000Z")
}

func intOr(p *int) string {
	if p == nil {
		return ""
	}
	return strconv.Itoa(*p)
}

func strOr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

/*
 * The Trakt shape: the body Trakt's history sync accepts, movies by title,
 * year and imdb id, episodes grouped under their show and season.
 *
 * An episode with no season or episode number cannot be placed under a show,
 * so it is left out of this file and kept in the CSV, which has no such
 * requirement. That count is reported as `skipped` so the difference is not a
 * silent one.
 */
type traktIDs struct {
	IMDb string `json:"imdb,omitempty"`
}

type traktMovie struct {
	WatchedAt string   `json:"watched_at"`
	Title     string   `json:"title"`
	Year      *int     `json:"year,omitempty"`
	IDs       traktIDs `json:"ids"`
}

type traktEpisode struct {
	Number    int    `json:"number"`
	WatchedAt string `json:"watched_at"`
}

type traktSeason struct {
	Number   int            `json:"number"`
	Episodes []traktEpisode `json:"episodes"`
}

type traktShow struct {
	Title   string        `json:"title"`
	Year    *int          `json:"year,omitempty"`
	IDs     traktIDs      `json:"ids"`
	Seasons []traktSeason `json:"seasons"`
}

type traktExport struct {
	Movies  []traktMovie `json:"movies"`
	Shows   []traktShow  `json:"shows"`
	Skipped int          `json:"skipped"`
}

func traktHistory(vs []store.Viewing) traktExport {
	out := traktExport{Movies: []traktMovie{}, Shows: []traktShow{}}
	showAt := map[string]int{}
	for _, v := range vs {
		switch v.Kind {
		case "movie":
			out.Movies = append(out.Movies, traktMovie{
				WatchedAt: watchedAt(v.FinishedAt), Title: v.Title, Year: v.Year,
				IDs: traktIDs{IMDb: strOr(v.IMDbID)},
			})
		case "episode":
			if v.Season == nil || v.Episode == nil || v.Series == nil {
				out.Skipped++
				continue
			}
			key := *v.Series + "\x00" + strOr(v.ShowIMDbID)
			i, ok := showAt[key]
			if !ok {
				i = len(out.Shows)
				showAt[key] = i
				out.Shows = append(out.Shows, traktShow{
					Title: *v.Series, Year: v.ShowYear, IDs: traktIDs{IMDb: strOr(v.ShowIMDbID)},
				})
			}
			sh := &out.Shows[i]
			j := -1
			for k := range sh.Seasons {
				if sh.Seasons[k].Number == *v.Season {
					j = k
				}
			}
			if j < 0 {
				sh.Seasons = append(sh.Seasons, traktSeason{Number: *v.Season})
				j = len(sh.Seasons) - 1
			}
			sh.Seasons[j].Episodes = append(sh.Seasons[j].Episodes,
				traktEpisode{Number: *v.Episode, WatchedAt: watchedAt(v.FinishedAt)})
		}
	}
	return out
}
