// Command lancastd is the LANcast media server.
//
// This file is wiring only — flags, construction, shutdown. Logic lives in
// internal packages.
package main

import (
	"context"
	"crypto/tls"
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
	"lancast/internal/meta/omdb"
	"lancast/internal/meta/tmdb"
	"lancast/internal/plugin"
	"lancast/internal/probe"
	"lancast/internal/scan"
	"lancast/internal/store"
	"lancast/internal/subtitle"
	"lancast/internal/tlscert"
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

	// Plugins (ADR 0020) are loaded once from the data dir and registered into
	// each rebuilt registry. The runtime hands them a secret resolver scoped to
	// what each plugin's manifest grants — a plugin never reads config directly.
	pluginRT, err := plugin.NewRuntime(context.Background(), log,
		plugin.WithSecretResolver(func(name string) string {
			s := settings.Get()
			switch name {
			case "omdb_key":
				return s.OMDbKey
			case "tmdb_key":
				return s.TMDBKey
			case "opensubtitles_key":
				return s.OpenSubtitlesKey
			default:
				return ""
			}
		}),
	)
	if err != nil {
		return fmt.Errorf("plugin runtime: %w", err)
	}
	defer pluginRT.Close(context.Background())
	installed, err := st.ListInstalledPlugins(context.Background())
	if err != nil {
		return fmt.Errorf("list installed plugins: %w", err)
	}
	records := make([]plugin.InstalledRecord, 0, len(installed))
	for _, ip := range installed {
		records = append(records, plugin.InstalledRecord{
			Name: ip.Name, Digest: ip.Digest, Enabled: ip.Enabled,
			GrantedHTTP: ip.GrantedHTTP, GrantedSecrets: ip.GrantedSecrets,
		})
	}
	plugins := pluginRT.LoadInstalled(context.Background(),
		filepath.Join(cfg.DataDir, "plugins"), records, plugin.KnownBadDigests)

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
		// External ratings (ADR 0019): registered only when a key is present, so
		// no key means the rating pass never runs and nothing phones home.
		if s.OMDbKey != "" {
			next.AddRatingSource(omdb.New(s.OMDbKey, omdb.WithCache(st)))
		}
		// Plugin-provided sources register the same way — indistinguishable
		// downstream from the native ones (ADR 0007).
		plugin.RegisterInto(next, plugins, log)
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
	// TLS encrypts the wire whenever the server is reachable beyond loopback, so
	// the password and session cookie no longer travel in plaintext on a
	// semi-trusted LAN. A loopback-only server stays plain HTTP: nothing on the
	// wire is worth protecting, and a certificate warning on localhost is pure
	// setup friction (ADR 0014).
	var tlsConfig *tls.Config
	if lanBound {
		cert, mode, err := loadTLSCert(cfg, settings.Get(), log)
		if err != nil {
			return err
		}
		tlsConfig = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
		log.Info("TLS enabled", "certificate", mode)
	}

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("cannot listen on %s: %w\n"+
			"Another LANcast may still be running — close it, or run: taskkill /IM lancastd.exe /F",
			listenAddr, err)
	}

	// redirectSrv only exists under TLS, to answer bookmarked http:// requests
	// with a redirect on the same port. Declared here so shutdown can reach it.
	var redirectSrv *http.Server

	errc := make(chan error, 1)
	if tlsConfig != nil {
		tlsLn, plainLn := splitTLS(listener, log)
		redirectSrv = &http.Server{
			Handler:           http.HandlerFunc(httpsRedirect),
			ReadHeaderTimeout: 10 * time.Second,
		}
		go func() {
			// Best-effort: a failing redirect listener must not take down HTTPS.
			if err := redirectSrv.Serve(plainLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Warn("http redirect listener stopped", "error", err)
			}
		}()
		go func() {
			log.Info("listening", "addr", listenAddr, "scheme", "https", "data", cfg.DataDir)
			if err := srv.Serve(tls.NewListener(tlsLn, tlsConfig)); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errc <- err
			}
		}()
	} else {
		go func() {
			log.Info("listening", "addr", listenAddr, "scheme", "http", "data", cfg.DataDir)
			if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errc <- err
			}
		}()
	}

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
	}

	cancelEnrich()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if redirectSrv != nil {
		_ = redirectSrv.Shutdown(shutdownCtx)
	}
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

// loadTLSCert returns the certificate to serve and a human-readable description
// of where it came from. A supplied cert (bring-your-own) is used verbatim;
// otherwise a self-signed certificate is generated and persisted under the data
// directory, covering loopback and the machine's LAN addresses (ADR 0014).
func loadTLSCert(cfg config.Config, s config.Settings, log *slog.Logger) (tls.Certificate, string, error) {
	if s.CustomTLS() {
		cert, err := tls.LoadX509KeyPair(s.TLSCertFile, s.TLSKeyFile)
		if err != nil {
			return tls.Certificate{}, "", fmt.Errorf("load TLS certificate from %s / %s: %w",
				s.TLSCertFile, s.TLSKeyFile, err)
		}
		return cert, "supplied", nil
	}

	dir := filepath.Join(cfg.DataDir, "tls")
	cert, err := tlscert.LoadOrGenerate(dir, tlscert.LocalIPs())
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf("prepare self-signed certificate: %w", err)
	}
	// Honest about what a self-signed cert does and does not buy, and how to
	// replace it — a first HTTPS visit will show a browser warning.
	log.Info("using a self-signed certificate; browsers will warn until it is trusted",
		"dir", dir, "hint", "set tls_cert_file and tls_key_file in settings to use your own certificate")
	return cert, "self-signed", nil
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
