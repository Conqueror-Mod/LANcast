package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"lancast/internal/store"
)

/*
 * What you have chosen about your Home page (ADR 0120, settings-3-plan
 * step 3):
 *
 *	GET /api/profile/home   yours: spotlight, its pinned title, spoilers,
 *	                        hidden built-in shelves
 *	PUT /api/profile/home   change any of them; a field left out is kept
 *
 * A null field has never been chosen, so the client shows the default and may
 * carry a device's older choice over once. `account` is false on a server with
 * no accounts yet, where there is nobody to keep a choice for and the client
 * keeps it on the device as it always did.
 */

var homeSpotlights = map[string]bool{"continue": true, "recent": true, "recommended": true, "pinned": true}

var homeSpoilers = map[string]bool{"show": true, "synopsis": true, "all": true}

// Built-in shelves that can be hidden by name. Unwatched is per library:
// "unwatched:<library id>".
var homeBuiltinShelves = map[string]bool{"new": true, "rate": true, "watchlist": true}

// maxHiddenShelves bounds the list: three named shelves and one Unwatched per
// library, with room to spare.
const maxHiddenShelves = 256

type homePrefsResponse struct {
	Account bool `json:"account"`
	store.HomePrefs
}

func (s *Server) homePreferences(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFromContext(r)
	if !ok {
		writeJSON(w, http.StatusOK, homePrefsResponse{HomePrefs: store.HomePrefs{HiddenShelves: []string{}}})
		return
	}
	p, err := s.st.HomePrefsFor(r.Context(), sess.UserID)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "no such account")
			return
		}
		s.writeInternal(w, err, "read home preferences")
		return
	}
	writeJSON(w, http.StatusOK, homePrefsResponse{Account: true, HomePrefs: p})
}

func (s *Server) setHomePreferences(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFromContext(r)
	if !ok {
		writeError(w, http.StatusConflict, "no_account",
			"this server has no accounts yet; create one first")
		return
	}
	var req struct {
		Spotlight     *string   `json:"spotlight"`
		SpotlightItem *int64    `json:"spotlight_item"`
		Spoilers      *string   `json:"spoilers"`
		HiddenShelves *[]string `json:"hidden_shelves"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		(req.Spotlight == nil && req.SpotlightItem == nil && req.Spoilers == nil && req.HiddenShelves == nil) {
		writeError(w, http.StatusBadRequest, "bad_request",
			"one of spotlight, spotlight_item, spoilers or hidden_shelves is required")
		return
	}
	if req.Spotlight != nil && !homeSpotlights[*req.Spotlight] {
		writeError(w, http.StatusBadRequest, "bad_spotlight",
			"spotlight must be continue, recent, recommended or pinned")
		return
	}
	if req.SpotlightItem != nil && *req.SpotlightItem < 0 {
		writeError(w, http.StatusBadRequest, "bad_spotlight_item", "spotlight_item must be 0 (none) or an item id")
		return
	}
	if req.Spoilers != nil && !homeSpoilers[*req.Spoilers] {
		writeError(w, http.StatusBadRequest, "bad_spoilers", "spoilers must be show, synopsis or all")
		return
	}
	u := store.HomePrefsUpdate{
		Spotlight: req.Spotlight, SpotlightItem: req.SpotlightItem, Spoilers: req.Spoilers,
	}
	if req.HiddenShelves != nil {
		keys := *req.HiddenShelves
		if len(keys) > maxHiddenShelves {
			writeError(w, http.StatusBadRequest, "bad_hidden_shelves", "too many hidden shelves")
			return
		}
		libs := map[string]int64{}
		for _, k := range keys {
			if homeBuiltinShelves[k] {
				continue
			}
			id, ok := unwatchedShelfLibrary(k)
			if !ok {
				writeError(w, http.StatusBadRequest, "bad_hidden_shelves",
					"a hidden shelf is new, rate, watchlist or unwatched:<library id>")
				return
			}
			// The row names its library so it goes when the library does; a
			// library that does not exist has no shelf to hide.
			if _, err := s.st.GetLibrary(r.Context(), id); err != nil {
				if err == store.ErrNotFound {
					writeError(w, http.StatusBadRequest, "bad_hidden_shelves", "no such library: "+k)
					return
				}
				s.writeInternal(w, err, "set home preferences")
				return
			}
			libs[k] = id
		}
		u.HiddenShelves = &keys
		u.HiddenLibraries = libs
	}
	if err := s.st.SetHomePrefs(r.Context(), sess.UserID, u); err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "no such account")
			return
		}
		s.writeInternal(w, err, "set home preferences")
		return
	}
	s.homePreferences(w, r)
}

// unwatchedShelfLibrary reads the library id out of "unwatched:<id>".
func unwatchedShelfLibrary(k string) (int64, bool) {
	rest, ok := strings.CutPrefix(k, "unwatched:")
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != rest {
		return 0, false
	}
	return id, true
}
