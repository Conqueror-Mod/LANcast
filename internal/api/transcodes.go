package api

import (
	"net/http"
	"sort"

	"lancast/internal/transcode"
)

/*
 * What the server is converting, right now.
 *
 * This exists because of a real morning spent without it. A film refused to
 * play, the app said only that it could not, and the answer — three sessions
 * holding every slot, none of which had ever delivered a byte to anybody — was
 * available exclusively by reading lancastd.log by hand. The server knew
 * everything needed to explain itself and had no way to say it.
 *
 * Administrator only, and that is a privacy decision rather than a permissions
 * afterthought. A session names an account and a film, so a list of them is a
 * list of who is watching what. An administrator can already read the audit
 * log; a member cannot, and must not learn from a diagnostics panel what tags
 * and history are careful to keep to themselves.
 */

// transcodeView is one conversion, as the settings page shows it.
type transcodeView struct {
	ID     string `json:"id"`
	ItemID int64  `json:"item_id"`
	// Title is resolved here rather than in the page, which has no way to look
	// up an item it is not already showing. Empty when the item has gone.
	Title string `json:"title,omitempty"`
	Owner string `json:"owner,omitempty"`
	// Live marks a channel rather than a library item. Channel ids are recorded
	// negated, the convention LiveHLS establishes, so the two numbering schemes
	// cannot be confused for one another.
	Live     bool   `json:"live"`
	Output   string `json:"output"`
	Encoding bool   `json:"encoding"`
	// StartAt is the offset into the film this conversion began at, which is
	// what tells a seek apart from a fresh start.
	StartAt        float64 `json:"start_at"`
	IdleSeconds    int     `json:"idle_seconds"`
	RunningSeconds int     `json:"running_seconds"`
	/*
	 * ServedBytes is the field this panel exists for.
	 *
	 * Zero means the slot is being held for nobody: the session was started,
	 * never read from, and is keeping somebody else from playing anything.
	 * Everything else here is context for that number.
	 */
	ServedBytes int64  `json:"served_bytes"`
	Finished    bool   `json:"finished"`
	Error       string `json:"error,omitempty"`
}

type transcodesView struct {
	// Max is the ceiling, so the page can say "two of three" rather than "two",
	// which is the difference between information and a number.
	Max      int             `json:"max"`
	Sessions []transcodeView `json:"sessions"`
}

func (s *Server) transcodes(w http.ResponseWriter, r *http.Request) {
	if s.trans == nil {
		// No transcoder configured. An empty list is the honest answer and
		// keeps the page from having to distinguish "none" from "cannot know".
		writeJSON(w, http.StatusOK, transcodesView{Sessions: []transcodeView{}})
		return
	}

	title := func(itemID int64) string {
		item, err := s.st.GetItem(r.Context(), itemID, s.userID(r))
		if err != nil || item == nil {
			return ""
		}
		return item.Title
	}
	writeJSON(w, http.StatusOK, transcodesView{
		Max:      s.trans.MaxSessions,
		Sessions: transcodeViews(s.trans.Sessions(), title),
	})
}

/*
 * transcodeViews turns the manager's sessions into what the page shows.
 *
 * Separated from the handler, and taking the title lookup as a function, so the
 * ordering and the channel rule can be tested without a database, a request, or
 * an ffmpeg — which is the same split the rest of this project keeps between a
 * decision and the machinery around it.
 */
func transcodeViews(infos []transcode.SessionInfo, title func(int64) string) []transcodeView {
	out := make([]transcodeView, 0, len(infos))
	for _, in := range infos {
		v := transcodeView{
			ID: in.ID, ItemID: in.ItemID, Owner: in.Owner,
			Output: in.Output, Encoding: in.Encoding, StartAt: in.StartAt,
			IdleSeconds: in.IdleSeconds, RunningSeconds: in.RunningSeconds,
			ServedBytes: in.ServedBytes, Finished: in.Finished, Error: in.Error,
		}
		/*
		 * A channel is not a library item, and asking the database for one by
		 * its negated id would either find nothing or — worse — find the item
		 * that happens to carry that number.
		 */
		if in.ItemID < 0 {
			v.Live = true
		} else if title != nil {
			v.Title = title(in.ItemID)
		}
		out = append(out, v)
	}

	/*
	 * Idle first.
	 *
	 * The list is read when something has just been refused, and the row worth
	 * looking at then is the one nobody is using. Sorting by age would put the
	 * film somebody is actually watching at the top of a list whose whole
	 * purpose is deciding what to stop.
	 */
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IdleSeconds != out[j].IdleSeconds {
			return out[i].IdleSeconds > out[j].IdleSeconds
		}
		return out[i].ID < out[j].ID
	})
	return out
}

/*
 * stopTranscode ends one conversion.
 *
 * The point of showing the list is being able to act on it. Without this the
 * panel reports a slot held by nobody and offers waiting ten minutes as the
 * remedy, which is what the person reading it was already doing.
 *
 * A session that has already gone answers 404 rather than an error: two
 * administrators pressing Stop on the same row is not a failure, and the second
 * one has got what they asked for.
 */
func (s *Server) stopTranscode(w http.ResponseWriter, r *http.Request) {
	if s.trans == nil {
		writeError(w, http.StatusNotFound, "not_found", "no such transcode")
		return
	}
	id := r.PathValue("id")
	if id == "" || !s.trans.StopSession(id) {
		writeError(w, http.StatusNotFound, "not_found", "no such transcode")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
