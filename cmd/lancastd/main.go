// Command lancastd is the LANcast media server.
//
// This file is wiring only — flags, construction, shutdown. Logic lives in
// internal packages.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"lancast/internal/api"
	"lancast/internal/artwork"
	"lancast/internal/config"
	"lancast/internal/enrich"
	"lancast/internal/meta"
	"lancast/internal/meta/nfo"
	"lancast/internal/meta/tmdb"
	"lancast/internal/probe"
	"lancast/internal/scan"
	"lancast/internal/store"
	"lancast/internal/subtitle"
	"lancast/internal/transcode"
	"lancast/internal/web"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dataDir := flag.String("data", "", "data directory (default: per-user config dir)")
	verbose := flag.Bool("v", false, "verbose logging")
	flag.Parse()

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	if err := run(*addr, *dataDir, log); err != nil {
		log.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run(addr, dataDir string, log *slog.Logger) error {
	cfg, err := config.Resolve(addr, dataDir)
	if err != nil {
		return err
	}

	settings, err := config.LoadSettings(cfg.DataDir)
	if err != nil {
		return err
	}

	st, err := store.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer st.Close()

	version, err := st.SchemaVersion()
	if err != nil {
		return err
	}
	log.Info("database ready", "path", cfg.DBPath(), "schema", version)

	// Multi-user migration completion (ADR 0015). Migration 7 only creates the
	// user table; it cannot read config.json, so the pre-existing single password
	// is turned into a 'local' admin here. The 'local' id matches the default
	// every session and playback_state row already carries, so nothing is
	// orphaned.
	if err := seedLegacyOwner(st, settings, log); err != nil {
		return err
	}

	scanner := scan.New(st, log)
	art := artwork.New(filepath.Join(cfg.DataDir, "artwork"))

	// The registry is rebuilt whenever settings change so a newly entered API
	// key takes effect without a restart.
	reg := meta.NewRegistry()
	var regMu sync.Mutex
	rebuild := func(s config.Settings) {
		regMu.Lock()
		defer regMu.Unlock()

		next := meta.NewRegistry()
		next.AddLocal(nfo.New())
		if s.TMDBKey != "" {
			next.AddProvider(tmdb.New(s.TMDBKey,
				tmdb.WithCache(st),
				tmdb.WithLimiter(meta.NewLimiter(s.RatePerSec, int(s.RatePerSec)+1)),
			))
		}
		*reg = *next
	}
	rebuild(settings.Get())

	if settings.Get().TMDBKey == "" {
		// Not a warning: running without a key is a supported configuration,
		// and saying so plainly is what makes the no-phone-home promise real.
		log.Info("no metadata provider configured; using filename and NFO metadata only")
	}

	worker := newWorker(st, reg, art, settings, log)

	prober := probe.New()
	probes := probe.NewWorker(st, prober, log)

	subs := subtitle.NewExtractor(filepath.Join(cfg.DataDir, "subtitles"))
	trans := transcode.NewManager(filepath.Join(cfg.DataDir, "transcode"), log)
	if !trans.Available() {
		log.Info("ffmpeg not found; files that cannot be played directly will not be converted")
	}
	if !prober.Available() {
		// Supported, not broken: LANcast serves files fine without ffmpeg, it
		// just cannot tell whether a client can play them.
		log.Info("ffprobe not found; playback decisions will assume direct play",
			"hint", "install ffmpeg to enable codec detection")
	}

	// enrichSoon runs a pass in the background, coalescing concurrent requests.
	var enrichMu sync.Mutex
	enrichCtx, cancelEnrich := context.WithCancel(context.Background())
	defer cancelEnrich()

	enrichSoon := func() {
		if !settings.Get().AutoEnrich {
			return
		}
		go func() {
			// Worker.Run already no-ops if a pass is in flight; this mutex
			// just keeps the goroutine count down.
			enrichMu.Lock()
			defer enrichMu.Unlock()
			if err := worker.Run(enrichCtx); err != nil && !errors.Is(err, context.Canceled) {
				log.Warn("enrichment pass failed", "error", err)
			}
		}()
	}

	// Probing runs independently of metadata enrichment: it is local, needs no
	// API key, and playback decisions depend on it. Tying it to enrichment
	// would mean a library with no TMDB key never gets probed.
	var probeMu sync.Mutex
	probeSoon := func() {
		go func() {
			probeMu.Lock()
			defer probeMu.Unlock()
			if err := probes.Run(enrichCtx); err != nil && !errors.Is(err, context.Canceled) {
				log.Warn("probe pass failed", "error", err)
			}
		}()
	}

	// Safe by default: an unsecured server does not listen beyond loopback.
	//
	// Rejecting LAN requests after accepting them would still mean the port is
	// open and answering. Not binding at all makes accidental exposure
	// impossible rather than merely discouraged.
	userCount, err := st.CountUsers(context.Background())
	if err != nil {
		return err
	}
	listenAddr, lanBound := bindAddr(cfg.Addr, userCount > 0)
	if !lanBound {
		log.Warn("no account set — listening on loopback only",
			"addr", listenAddr, "hint", "create an account in the browser, then restart to reach LANcast from other devices")
	}

	srv := &http.Server{
		Addr: listenAddr,
		Handler: api.New(api.Deps{
			LANBound: lanBound,
			Store:    st, Scanner: scanner, Registry: reg, Artwork: art,
			Worker: worker, Probes: probes, Trans: trans, Subs: subs,
			Settings: settings, DataDir: cfg.DataDir, Log: log, Web: web.Handler(),
			Rebuild: func(s config.Settings) {
				rebuild(s)
				worker.SetNFOWriter(nfoWriterFor(s))
			},
			Enrich: enrichSoon,
		}).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// No write timeout: streaming a film is a legitimately long response.
	}

	// A scan produces pending items; both workers consume them. Without this
	// wiring a fresh scan stays unenriched and unprobed until the next restart.
	scanner.OnFinish(func() {
		probeSoon()
		enrichSoon()
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Clears leftover scratch from a previous run and starts reaping idle
	// sessions. A closed browser tab does not tell the server it has gone.
	trans.Start(ctx)
	defer trans.StopAll()

	// Detection runs a real test encode per candidate, because ffmpeg lists
	// encoders the machine cannot run. Done in the background so a slow or
	// wedged GPU driver delays transcoding rather than startup.
	if trans.Available() {
		go trans.DetectHardware(ctx, settings.Get().HardwareEncoder)
	}

	// Pick up anything left pending from a previous run.
	probeSoon()
	enrichSoon()

	// Bind before serving so a port clash is a clear startup failure rather
	// than a background error nobody sees. An older instance still holding the
	// port is the failure that looks like "my changes did nothing": the new
	// process dies and the old build keeps answering the browser.
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("cannot listen on %s: %w\n"+
			"Another LANcast may still be running — close it, or run: taskkill /IM lancastd.exe /F",
			listenAddr, err)
	}

	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", listenAddr, "data", cfg.DataDir)
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
	}

	cancelEnrich()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// bindAddr forces an unsecured server onto loopback, preserving the requested
// port. It reports whether the result is reachable from the network.
func bindAddr(requested string, secured bool) (addr string, lanBound bool) {
	if secured {
		return requested, true
	}

	_, port, err := net.SplitHostPort(requested)
	if err != nil {
		// A bare port like ":8080" or something unparseable: fall back to a
		// known-safe address rather than guessing.
		return "127.0.0.1:8080", false
	}
	return net.JoinHostPort("127.0.0.1", port), false
}

// seedLegacyOwner completes the multi-user migration (ADR 0015): with no
// accounts but a legacy single password still in config.json, that password
// becomes the 'local' admin and is then cleared from settings, so credentials
// have one home rather than two. A fresh install has neither and is left for
// setup to create the first admin.
func seedLegacyOwner(st *store.Store, settings *config.SettingsStore, log *slog.Logger) error {
	ctx := context.Background()
	n, err := st.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	cur := settings.Get()
	if cur.PasswordHash == "" {
		return nil
	}
	if _, err := st.CreateUser(ctx, store.LocalUserID, "admin", cur.PasswordHash, store.RoleAdmin); err != nil {
		return fmt.Errorf("seed local admin: %w", err)
	}
	cur.PasswordHash = ""
	if err := settings.Set(cur); err != nil {
		return fmt.Errorf("clear legacy password: %w", err)
	}
	log.Info("migrated the single password to a 'local' admin account", "name", "admin")
	return nil
}

func newWorker(st *store.Store, reg *meta.Registry, art *artwork.Cache,
	settings *config.SettingsStore, log *slog.Logger) *enrich.Worker {

	w := enrich.New(st, reg, art, log)
	w.SetNFOWriter(nfoWriterFor(settings.Get()))
	return w
}

// nfoWriterFor returns a sidecar writer only when the user has opted in.
// Writing into someone's media folders is not something to do unasked.
func nfoWriterFor(s config.Settings) func(string, meta.Kind, *meta.Record) error {
	if !s.WriteNFO {
		return nil
	}
	src := nfo.New()
	return src.Write
}
