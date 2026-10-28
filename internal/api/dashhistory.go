package api

import (
	"context"
	"net/http"
	"runtime/metrics"
	"strconv"
	"time"

	"lancast/internal/history"
)

/*
 * Dashboard history (ADR 0106): a sample of the server every minute, from
 * what it already keeps (ADR 0096): presence, the transcode manager, the
 * request counters, and the machine's CPU. Kept 48 hours, in memory, saved
 * beside the database every five minutes and on the way out.
 *
 * No bandwidth. The server counts the bytes conversions hand out and nothing
 * else, and counting every response would mean wrapping the response writer,
 * which ADR 0096 declined because the event stream needs its Flush.
 */

// historySaveEvery is how often the ring is written to disk.
const historySaveEvery = 5 * time.Minute

// RecordHistory samples the server every minute until ctx ends, saving the
// history to dataDir. The server program (internal/serverapp) starts it once.
func (s *Server) RecordHistory(ctx context.Context, dataDir string) {
	path := history.File(dataDir)
	s.history.LoadInto(path, time.Now())
	var cpu history.CPU
	cpu.Percent() // the first reading only sets the baseline
	lastReq, lastErr := s.counters.requests.Load(), s.counters.serverErrors.Load()
	tick := time.NewTicker(history.Every)
	defer tick.Stop()
	lastSave := time.Now()
	for {
		select {
		case <-ctx.Done():
			_ = s.history.Save(path)
			return
		case now := <-tick.C:
			req, errs := s.counters.requests.Load(), s.counters.serverErrors.Load()
			s.history.Add(s.sampleNow(now, cpu.Percent(), req-lastReq, errs-lastErr))
			lastReq, lastErr = req, errs
			if now.Sub(lastSave) >= historySaveEvery {
				if err := s.history.Save(path); err != nil {
					s.log.Warn("could not save the dashboard history", "error", err)
				}
				lastSave = now
			}
		}
	}
}

// sampleNow is one minute's sample, read from what the server keeps.
func (s *Server) sampleNow(now time.Time, cpu float64, requests, errs int64) history.Sample {
	smp := history.Sample{At: now.Unix(), CPU: cpu, Requests: requests, Errors: errs}
	for _, id := range s.presence.Online() {
		if st, ok := s.presence.Snapshot(id); ok && st.Watching != "" {
			smp.Watching++
		}
	}
	if s.trans != nil {
		for _, ss := range s.trans.Sessions() {
			switch {
			case ss.Finished:
			case ss.Encoding:
				smp.Encoding++
			default:
				smp.Copying++
			}
		}
	}
	// What the Go runtime holds from the operating system, read without the
	// stop-the-world pause runtime.ReadMemStats takes.
	m := []metrics.Sample{{Name: "/memory/classes/total:bytes"}}
	metrics.Read(m)
	if m[0].Value.Kind() == metrics.KindUint64 {
		smp.MemoryMB = float64(m[0].Value.Uint64()) / (1 << 20)
	}
	return smp
}

// dashboardHistory answers GET /api/dashboard/history?hours=N: the samples
// of the last N hours (1 to 48, default 6), oldest first. Admin-only.
func (s *Server) dashboardHistory(w http.ResponseWriter, r *http.Request) {
	hours := 6
	if v := r.URL.Query().Get("hours"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > int(history.Keep/time.Hour) {
			writeError(w, http.StatusBadRequest, "bad_request", "hours is 1 to 48")
			return
		}
		hours = n
	}
	samples := s.history.Since(time.Now().Add(-time.Duration(hours) * time.Hour))
	writeJSON(w, http.StatusOK, map[string]any{
		"every_seconds": int(history.Every / time.Second),
		"samples":       samples,
	})
}
