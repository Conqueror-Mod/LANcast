package api

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"lancast/internal/auth"
	"lancast/internal/backup"
	"lancast/internal/config"
	"lancast/internal/scan"
)

/*
 * Metrics: Prometheus text format, for people who already run Grafana
 * (ADR 0096).
 *
 * Who may read them. An administrator's session, or the **metrics token**: a
 * secret an administrator generates in Settings, shown once, stored hashed,
 * that reads this one route and nothing else. Not an API key, because ADR
 * 0061 refuses keys anything an administrator can do, and these numbers —
 * how many accounts, which libraries hold what, whose conversions are running
 * — are an administrator's view of the server. A scraper is unattended and
 * kept in a config file on another machine; giving it a credential that opens
 * exactly one read is the narrowest thing that works.
 *
 * Written by hand rather than with the Prometheus client library: the format
 * is a few lines of text, and the library would be the largest dependency
 * this package carries for the sake of a page nobody has to look at.
 *
 * Every value is read when scraped, from state the server already keeps. The
 * only things counted for this alone are requests and server errors.
 */

// metricsCounters are the two numbers kept only so they can be reported.
type metricsCounters struct {
	requests     atomic.Int64
	serverErrors atomic.Int64
	// mediaBytes is what the media routes have written (mediabytes.go).
	mediaBytes atomic.Int64
}

// metricsTokenPrefix tells a metrics token apart from an API key at a glance,
// in a config file or in a leak report.
const metricsTokenPrefix = "lcm_"

// metricsCtxKey marks a request authenticated by the metrics token.
type metricsCtxKeyT struct{}

var metricsCtxKey metricsCtxKeyT

// metricsToken reports whether the request carries the metrics token. Only
// ever asked about GET /api/metrics; anywhere else the token means nothing.
func (s *Server) metricsToken(r *http.Request) bool {
	stored := s.settings.Get().MetricsTokenHash
	if stored == "" {
		return false
	}
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return false
	}
	token := strings.TrimSpace(h[len(prefix):])
	if !strings.HasPrefix(token, metricsTokenPrefix) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(auth.HashToken(token)), []byte(stored)) == 1
}

// metricsRoute serves the metrics to the token or to an administrator.
func (s *Server) metricsRoute(w http.ResponseWriter, r *http.Request) {
	if ok, _ := r.Context().Value(metricsCtxKey).(bool); ok {
		s.writeMetrics(w, r)
		return
	}
	s.adminOnly(s.writeMetrics)(w, r)
}

// createMetricsToken makes a new token, replacing any old one. Admin only.
// The token is in this answer and nowhere else, ever.
func (s *Server) createMetricsToken(w http.ResponseWriter, r *http.Request) {
	raw, _, err := auth.NewToken()
	if err != nil {
		s.writeInternal(w, err, "generate metrics token")
		return
	}
	token := metricsTokenPrefix + raw
	cur := s.settings.Get()
	replaced := cur.MetricsTokenHash != ""
	cur.MetricsTokenHash = auth.HashToken(token)
	cur.MetricsTokenCreated = time.Now().Unix()
	if err := s.settings.Set(cur); err != nil {
		s.writeInternal(w, err, "save metrics token")
		return
	}
	summary := "Created the metrics token"
	if replaced {
		summary = "Replaced the metrics token"
	}
	s.audit(r, "metrics.token", "", "", summary, nil)
	writeJSON(w, http.StatusCreated, map[string]any{
		"token":      token,
		"created_at": cur.MetricsTokenCreated,
	})
}

// deleteMetricsToken revokes it. Admin only. Idempotent.
func (s *Server) deleteMetricsToken(w http.ResponseWriter, r *http.Request) {
	cur := s.settings.Get()
	if cur.MetricsTokenHash != "" {
		cur.MetricsTokenHash = ""
		cur.MetricsTokenCreated = 0
		if err := s.settings.Set(cur); err != nil {
			s.writeInternal(w, err, "revoke metrics token")
			return
		}
		s.audit(r, "metrics.token", "", "", "Revoked the metrics token", nil)
	}
	w.WriteHeader(http.StatusNoContent)
}

/*
 * promWriter collects the text exposition format and writes it grouped.
 *
 * Every sample of a family has to sit in one contiguous block under its HELP
 * and TYPE lines; a scraper given the same family in two places rejects it or
 * keeps only part. Samples are therefore buffered per family in the order the
 * families were first named, and written out together at the end. Writing
 * them as they came interleaved the per-library families, which is how this
 * was found, against a real three-library server.
 */
type promWriter struct {
	order   []string
	header  map[string]string
	samples map[string]*strings.Builder
}

func (p *promWriter) family(name, kind, help string) {
	if p.header == nil {
		p.header = map[string]string{}
		p.samples = map[string]*strings.Builder{}
	}
	if _, ok := p.header[name]; ok {
		return
	}
	p.order = append(p.order, name)
	p.header[name] = fmt.Sprintf("# HELP %s %s\n# TYPE %s %s\n", name, help, name, kind)
	p.samples[name] = &strings.Builder{}
}

// sample adds one value to its family. labels alternate name, value.
func (p *promWriter) sample(name string, value float64, labels ...string) {
	b := p.samples[name]
	if b == nil {
		// A sample for a family never named is a programming error; it is
		// dropped rather than written somewhere it would break the format.
		return
	}
	b.WriteString(name)
	if len(labels) > 0 {
		b.WriteByte('{')
		for i := 0; i+1 < len(labels); i += 2 {
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(b, "%s=\"%s\"", labels[i], escapeLabel(labels[i+1]))
		}
		b.WriteByte('}')
	}
	fmt.Fprintf(b, " %s\n", formatFloat(value))
}

// String is the whole page, each family's samples in one block.
func (p *promWriter) String() string {
	var out strings.Builder
	for _, name := range p.order {
		out.WriteString(p.header[name])
		out.WriteString(p.samples[name].String())
	}
	return out.String()
}

// escapeLabel escapes a label value as the format requires: backslash,
// double quote and newline. Library names are typed by people.
func escapeLabel(v string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(v)
}

func formatFloat(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%g", v)
}

// writeMetrics renders every family. A part that cannot be read is left out
// rather than failing the scrape: a gap in one graph is better than every
// graph going blank because one count timed out.
func (s *Server) writeMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var p promWriter

	p.family("lancast_build_info", "gauge", "The running build. Always 1; the version is the label.")
	p.sample("lancast_build_info", 1, "version", Version, "api_version", fmt.Sprint(APIVersion))

	if !s.started.IsZero() {
		p.family("lancast_uptime_seconds", "gauge", "Seconds since this server started.")
		p.sample("lancast_uptime_seconds", float64(int64(time.Since(s.started).Seconds())))
	}

	p.family("lancast_http_requests_total", "counter", "HTTP requests received since start.")
	p.sample("lancast_http_requests_total", float64(s.counters.requests.Load()))
	p.family("lancast_http_server_errors_total", "counter",
		"Requests answered with an unexpected server error since start. A client that went away is not counted.")
	p.sample("lancast_http_server_errors_total", float64(s.counters.serverErrors.Load()))
	p.family("lancast_http_media_bytes_total", "counter",
		"Bytes of media sent since start: streams, conversions, downloads, extras, pre-roll, live channels, and the same for and through paired servers.")
	p.sample("lancast_http_media_bytes_total", float64(s.counters.mediaBytes.Load()))

	s.writeTranscodeMetrics(&p)
	s.writeLibraryMetrics(ctx, &p)

	if n, err := s.st.CountUsers(ctx); err == nil {
		p.family("lancast_accounts", "gauge", "Accounts on this server.")
		p.sample("lancast_accounts", float64(n))
	}
	if n, err := s.st.CountSessions(ctx); err == nil {
		p.family("lancast_sessions", "gauge", "Signed-in sessions that have not expired.")
		p.sample("lancast_sessions", float64(n))
	}

	s.writeBackupMetrics(&p)

	if s.dataDir != "" {
		var size int64
		for _, suffix := range []string{"", "-wal"} {
			if fi, err := os.Stat((config.Config{DataDir: s.dataDir}).DBPath() + suffix); err == nil {
				size += fi.Size()
			}
		}
		if size > 0 {
			p.family("lancast_database_bytes", "gauge", "Size of the database and its write-ahead log.")
			p.sample("lancast_database_bytes", float64(size))
		}
	}

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	p.family("go_goroutines", "gauge", "Goroutines running.")
	p.sample("go_goroutines", float64(runtime.NumGoroutine()))
	p.family("go_memstats_heap_alloc_bytes", "gauge", "Bytes of allocated heap objects.")
	p.sample("go_memstats_heap_alloc_bytes", float64(ms.HeapAlloc))
	p.family("go_memstats_sys_bytes", "gauge", "Bytes obtained from the operating system.")
	p.sample("go_memstats_sys_bytes", float64(ms.Sys))

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(p.String()))
}

func (s *Server) writeTranscodeMetrics(p *promWriter) {
	if s.trans == nil {
		return
	}
	encoding, copying := 0, 0
	var served int64
	for _, ss := range s.trans.Sessions() {
		if ss.Finished {
			continue
		}
		if ss.Encoding {
			encoding++
		} else {
			copying++
		}
		served += ss.ServedBytes
	}
	p.family("lancast_transcode_sessions", "gauge",
		"Conversions running now. mode=encode re-encodes the picture; mode=copy only rewraps it.")
	p.sample("lancast_transcode_sessions", float64(encoding), "mode", "encode")
	p.sample("lancast_transcode_sessions", float64(copying), "mode", "copy")
	p.family("lancast_transcode_sessions_max", "gauge", "The most conversions allowed at once (max_transcodes).")
	p.sample("lancast_transcode_sessions_max", float64(s.settings.Get().MaxTranscodes))
	p.family("lancast_transcode_served_bytes", "gauge", "Bytes the running conversions have handed to players.")
	p.sample("lancast_transcode_served_bytes", float64(served))
}

func (s *Server) writeLibraryMetrics(ctx context.Context, p *promWriter) {
	libs, err := s.st.ListLibraries(ctx)
	if err != nil {
		return
	}
	sort.Slice(libs, func(i, j int) bool { return libs[i].ID < libs[j].ID })
	for _, lib := range libs {
		id := fmt.Sprint(lib.ID)
		if shape, err := s.st.Shape(ctx, lib.ID); err == nil {
			p.family("lancast_library_items", "gauge", "Items present in a library (missing files excluded), by kind.")
			for _, k := range []struct {
				kind string
				n    int
			}{
				{"movie", shape.Movies}, {"show", shape.Shows}, {"season", shape.Seasons},
				{"episode", shape.Episodes}, {"part", shape.Parts}, {"track", shape.Tracks},
				{"photo", shape.Photos},
			} {
				if k.n > 0 {
					p.sample("lancast_library_items", float64(k.n), "library_id", id, "library", lib.Name, "kind", k.kind)
				}
			}
		}
		if s.scanner == nil {
			continue
		}
		st := s.scanner.Status(lib.ID)
		if st.FinishedAt == nil {
			continue
		}
		p.family("lancast_scan_last_finished_timestamp_seconds", "gauge", "When the library's last scan finished, as a Unix time.")
		p.sample("lancast_scan_last_finished_timestamp_seconds", float64(*st.FinishedAt), "library_id", id, "library", lib.Name)
		p.family("lancast_scan_last_duration_seconds", "gauge", "How long the library's last scan took.")
		p.sample("lancast_scan_last_duration_seconds", float64(st.ElapsedMS)/1000, "library_id", id, "library", lib.Name)
		p.family("lancast_scan_last_success", "gauge", "1 when the library's last scan completed, 0 when it failed or was cancelled.")
		ok := 0.0
		if st.State == scan.StateIdle && st.Error == "" {
			ok = 1
		}
		p.sample("lancast_scan_last_success", ok, "library_id", id, "library", lib.Name)
	}
}

func (s *Server) writeBackupMetrics(p *promWriter) {
	if s.dataDir == "" {
		return
	}
	files, err := backup.New(s.dataDir).List()
	if err != nil {
		return
	}
	p.family("lancast_backups", "gauge", "Backups in the backups folder.")
	p.sample("lancast_backups", float64(len(files)))
	if len(files) > 0 {
		p.family("lancast_backup_last_timestamp_seconds", "gauge", "When the newest backup was taken, as a Unix time.")
		p.sample("lancast_backup_last_timestamp_seconds", float64(files[0].TakenAt))
	}
}
