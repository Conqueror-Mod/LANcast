package api

import (
	"net/http"
	"strconv"

	"lancast/internal/applog"
)

// defaultLogLines is what a caller gets without asking. Enough to cover a
// startup and a failure after it; not so much that the panel takes a moment to
// render on a machine that has been up for weeks.
const defaultLogLines = 300

// maxLogLines caps what one request can pull. The log rolls at 4 MB, so this is
// not a memory bound so much as a "you asked for the file, download the file"
// boundary — reading it whole is what the data directory is for.
const maxLogLines = 2000

// serverLog returns the tail of lancastd.log. Admin only: the log names
// filesystem paths, library roots and provider errors, which is server-operator
// information rather than viewer information.
//
// It exists because the log has been written beside the database since v0.4.2
// and could only be read by finding the data directory in a file manager —
// which is exactly the audience least able to do that, since the log matters
// most when the server is running as a service and something is wrong.
func (s *Server) serverLog(w http.ResponseWriter, r *http.Request) {
	n := defaultLogLines
	if v := r.URL.Query().Get("lines"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed < 1 {
			writeError(w, http.StatusBadRequest, "bad_request",
				"lines must be a positive whole number")
			return
		}
		n = min(parsed, maxLogLines)
	}

	lines, complete, err := applog.Tail(s.dataDir, n)
	if err != nil {
		s.writeInternal(w, err, "read server log")
		return
	}
	if lines == nil {
		lines = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"lines": lines,
		// complete is false when older lines exist that this response does not
		// carry. Saying so is the difference between "this is the log" and
		// "this is the end of the log", and a reader who assumes the first goes
		// looking for a startup line that was never withheld from them.
		"complete": complete,
		"path":     applog.FileName,
	})
}
