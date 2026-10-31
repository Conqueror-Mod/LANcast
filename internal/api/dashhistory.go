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
 * Media sent is counted on the media routes only (mediabytes.go), never by
 * wrapping every response: ADR 0096 kept the event stream unwrapped because it
 * needs its Flush, and the event stream is not a media route.
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
	lastMedia := s.counters.mediaBytes.Load()
	s.markHistory(time.Now(), lastReq, lastErr, lastMedia)
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
			media := s.counters.mediaBytes.Load()
			smp := s.sampleNow(now, cpu.Percent(), req-lastReq, errs-lastErr)
			smp.MediaBytes = media - lastMedia
			s.history.Add(smp)
			lastReq, lastErr, lastMedia = req, errs, media
			s.markHistory(now, req, errs, media)
			if now.Sub(lastSave) >= historySaveEvery {
				if err := s.history.Save(path); err != nil {
					s.log.Warn("could not save the dashboard history", "error", err)
				}
				lastSave = now
			}
		}
	}
}

// histMark is the counters where the last recorded minute ended.
type histMark struct {
	at               time.Time
	req, errs, media int64
}

func (s *Server) markHistory(at time.Time, req, errs, media int64) {
	s.histMu.Lock()
	s.histLast = histMark{at: at, req: req, errs: errs, media: media}
	s.histMu.Unlock()
}

// liveMinimum is how much of a minute must have passed before its counts are
// scaled to a per-minute rate; before that, three seconds of requests times
// twenty is noise, and the last whole minute's figures stand in.
const liveMinimum = 10 * time.Second

/*
 * liveSample is the minute in progress, so the dashboard moves while it is
 * watched rather than once a minute (a real complaint: it looked frozen until
 * the page was reloaded).
 *
 * What is a state is read now: people watching, conversions, memory, and the
 * CPU since the last live reading. What is a count over the minute (requests,
 * errors, media sent) is what has accumulated since the last recorded minute,
 * scaled to a whole minute, so it is in the same units as every sample beside
 * it. Nil until the recorder has run once.
 */
func (s *Server) liveSample(now time.Time) *history.Sample {
	s.histMu.Lock()
	last := s.histLast
	cpu := s.liveCPU.Percent()
	s.histMu.Unlock()
	if last.at.IsZero() {
		return nil
	}
	req, errs := s.counters.requests.Load(), s.counters.serverErrors.Load()
	media := s.counters.mediaBytes.Load()
	el := now.Sub(last.at)
	var smp history.Sample
	if el >= liveMinimum {
		scale := float64(time.Minute) / float64(el)
		smp = s.sampleNow(now, cpu, int64(float64(req-last.req)*scale+0.5), int64(float64(errs-last.errs)*scale+0.5))
		smp.MediaBytes = int64(float64(media-last.media)*scale + 0.5)
	} else {
		smp = s.sampleNow(now, cpu, 0, 0)
		if prev := s.history.Since(last.at.Add(-time.Second)); len(prev) > 0 {
			p := prev[len(prev)-1]
			smp.Requests, smp.Errors, smp.MediaBytes = p.Requests, p.Errors, p.MediaBytes
		}
	}
	return &smp
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
	now := time.Now()
	samples := s.history.Since(now.Add(-time.Duration(hours) * time.Hour))
	out := map[string]any{
		"every_seconds": int(history.Every / time.Second),
		"samples":       samples,
	}
	if live := s.liveSample(now); live != nil {
		out["now"] = live
	}
	writeJSON(w, http.StatusOK, out)
}
