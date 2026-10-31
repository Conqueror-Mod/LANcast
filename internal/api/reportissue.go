package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"lancast/internal/applog"
	"lancast/internal/report"
	"lancast/internal/store"
)

/*
 * Report an issue (ADR 0109): a form in the app whose Send posts the report
 * to the developer's webhook, from this server, at that moment and no other.
 *
 * Anyone signed in may send one; only an administrator may attach the server
 * log, which names paths, library roots, addresses and other people's
 * accounts. The server's log is the server operator's to share.
 *
 * Limited to five an hour for the whole server and one every 20 seconds, so
 * a stuck button or a script cannot turn one person's channel into a flood.
 */

const (
	reportsPerHour = 5
	reportGap      = 20 * time.Second
	reportLogLines = 2000
)

type reportLimiter struct {
	mu   sync.Mutex
	sent []time.Time
}

// take records a send now if the limits allow one, and says how long to
// wait if they do not.
func (l *reportLimiter) take(now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	kept := l.sent[:0]
	for _, t := range l.sent {
		if now.Sub(t) < time.Hour {
			kept = append(kept, t)
		}
	}
	l.sent = kept
	if n := len(l.sent); n > 0 && now.Sub(l.sent[n-1]) < reportGap {
		return false, reportGap - now.Sub(l.sent[n-1])
	}
	if len(l.sent) >= reportsPerHour {
		return false, time.Hour - now.Sub(l.sent[0])
	}
	l.sent = append(l.sent, now)
	return true, 0
}

func (s *Server) reportWebhook() string {
	if s.reportURL != nil {
		return s.reportURL()
	}
	return report.Webhook()
}

// reportRole is who is asking: an administrator, a member, or nobody allowed
// to send. A server with no accounts yet is its owner's, on loopback only.
func (s *Server) reportRole(r *http.Request) (admin, ok bool) {
	sess, signed := sessionFromContext(r)
	if !signed {
		return true, !s.secured(r.Context())
	}
	if authedByKey(r) {
		return false, false
	}
	return sess.Role == store.RoleAdmin, true
}

// reportInfo answers GET /api/report: whether this build can send, and
// whether this account may attach the server log.
func (s *Server) reportInfo(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.reportRole(r)
	if !ok {
		writeError(w, http.StatusForbidden, "forbidden", "sign in to report an issue")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"can_send":       s.reportWebhook() != "",
		"can_attach_log": admin,
	})
}

type reportRequest struct {
	What       string `json:"what"`
	Expected   string `json:"expected"`
	Steps      string `json:"steps"`
	Contact    string `json:"contact"`
	Client     string `json:"client"`
	IncludeLog bool   `json:"include_log"`
}

// sendReport answers POST /api/report.
func (s *Server) sendReport(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.reportRole(r)
	if !ok {
		writeError(w, http.StatusForbidden, "forbidden", "sign in to report an issue")
		return
	}
	var req reportRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "that is not a report")
		return
	}
	if strings.TrimSpace(req.What) == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "say what happened")
		return
	}
	for _, f := range []string{req.What, req.Expected, req.Steps} {
		if len(f) > report.MaxField {
			writeError(w, http.StatusBadRequest, "bad_request", "a field is too long; attach the rest as steps")
			return
		}
	}
	if len(req.Contact) > 200 || len(req.Client) > 100 {
		writeError(w, http.StatusBadRequest, "bad_request", "a field is too long")
		return
	}
	if req.IncludeLog && !admin {
		writeError(w, http.StatusForbidden, "forbidden", "only an administrator can attach the server log")
		return
	}
	url := s.reportWebhook()
	if url == "" {
		writeError(w, http.StatusServiceUnavailable, "unavailable",
			"this build of LANcast cannot send reports; copy the report instead")
		return
	}
	if allowed, wait := s.reports.take(time.Now()); !allowed {
		writeError(w, http.StatusTooManyRequests, "rate_limited",
			"a report was sent a moment ago; try again in "+wait.Round(time.Second).String())
		return
	}

	rep := report.Report{
		What: req.What, Expected: req.Expected, Steps: req.Steps, Contact: req.Contact,
		Version: Version, Platform: runtime.GOOS + "/" + runtime.GOARCH, Client: req.Client,
		Account: map[bool]string{true: "admin", false: "member"}[admin],
	}
	if req.IncludeLog {
		lines, _, err := applog.Tail(s.dataDir, reportLogLines)
		if err != nil {
			s.writeInternal(w, err, "read server log for a report")
			return
		}
		log := strings.Join(lines, "\n")
		if len(log) > report.MaxLogBytes {
			log = log[len(log)-report.MaxLogBytes:]
		}
		rep.Log = []byte(log)
	}

	if err := report.Send(r.Context(), &http.Client{}, url, rep); err != nil {
		s.log.Warn("problem report not sent", "error", err)
		msg := "the report could not be sent; check the connection and try again, or copy it instead"
		if errors.Is(err, report.ErrNoWebhook) {
			msg = "this build of LANcast cannot send reports"
		}
		writeError(w, http.StatusBadGateway, "not_sent", msg)
		return
	}
	// What was attached, not what was asked for: a server whose log goes to
	// a terminal has no file, and says so rather than claiming one went.
	attached := len(rep.Log) > 0
	s.audit(r, "report.send", "server", "", "Sent a problem report to the developer",
		map[string]any{"log_included": attached})
	writeJSON(w, http.StatusOK, map[string]any{"sent": true, "log_included": attached})
}
