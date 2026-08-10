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
	"strings"
	"sync"
	"syscall"
	"time"

	"lancast/internal/api"
	"lancast/internal/artwork"
	"lancast/internal/config"
	"lancast/internal/coverart"
	"lancast/internal/enrich"
	"lancast/internal/mediatools"
	"lancast/internal/meta"
	"lancast/internal/meta/nfo"
	"lancast/internal/meta/omdb"
	"lancast/internal/meta/tmdb"
	"lancast/internal/photo"
	"lancast/internal/plugin"
	"lancast/internal/probe"
	"lancast/internal/scan"
	"lancast/internal/selfupdate"
	"lancast/internal/service"
	"lancast/internal/singleton"
	"lancast/internal/store"
	"lancast/internal/subtitle"
	"lancast/internal/tlscert"
	"lancast/internal/transcode"
	"lancast/internal/update"
	"lancast/internal/web"
)

func main() {
	// Subcommands come before flags: `lancastd service install -data …`. Routed
	// here so flag.Parse never sees the subcommand token.
	if len(os.Args) > 1 && os.Args[1] == "service" {
		if err := runService(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "lancastd service:", err)
			os.Exit(1)
		}
		return
	}

	// `lancastd devseed` points a development instance at the test libraries.
	// Present only in builds made with -tags devseed.
	if len(os.Args) > 1 && os.Args[1] == "devseed" {
		attachConsole()
		if err := runDevSeed(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "lancastd devseed:", err)
			os.Exit(1)
		}
		return
	}

	// `lancastd reset-auth` is the lockout recovery path. Console output only,
	// so it attaches to the caller's terminal first — a windowsgui build has no
	// console of its own and would otherwise print nothing at all.
	if len(os.Args) > 1 && os.Args[1] == "reset-auth" {
		attachConsole()
		if err := runResetAuth(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "lancastd reset-auth:", err)
			os.Exit(1)
		}
		return
	}

	// `lancastd tray` is the windowless desktop mode: the server plus a
	// system-tray presence (ADR 0022). A shortcut or the launcher invokes it.
	if len(os.Args) > 1 && os.Args[1] == "tray" {
		if err := runTray(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "lancastd tray:", err)
			os.Exit(1)
		}
		return
	}

	// A bare launch — a double-click, no arguments — has no console on Windows.
	// Running the server foreground there would start an invisible process the
	// user can neither see nor stop, so show the tray instead (ADR 0022).
	if len(os.Args) == 1 && bareLaunchUsesTray {
		if err := runTray(nil); err != nil {
			alert("LANcast", err.Error())
			os.Exit(1)
		}
		return
	}

	addr := flag.String("addr", ":8080", "listen address")
	dataDir := flag.String("data", "", "data directory (default: per-user config dir)")
	verbose := flag.Bool("v", false, "verbose logging")
	version := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	// -version answers "which build is this?" without starting anything — the
	// same string GET /api/health reports, injected at release build (ADR 0016).
	if *version {
		// Same console problem as reset-auth: a windowsgui build printing to a
		// stdout nobody attached looks like -version does nothing.
		attachConsole()
		fmt.Println(api.Version)
		return
	}

	// Explicit foreground run. Attach to the caller's terminal before the first
	// log line: a windowsgui build has no console of its own, so every message
	// — including the one explaining a refusal to start — goes nowhere, and the
	// server looks like it exited silently. Running it by hand is the way to
	// find out why a service died, and that only works if it can speak.
	attachConsole()

	log := newLogger(*verbose)

	// One server at a time. A second instance says so and exits rather than
	// racing the first for the port and the database.
	release, held, err := singleton.Acquire(singleton.Server)
	if err == nil && !held {
		logAlreadyRunning(log)
		os.Exit(1)
	}
	defer release()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, *addr, *dataDir, log); err != nil {
		log.Error("fatal", "error", err)
		os.Exit(1)
	}
}

// logAlreadyRunning reports the refusal to start with the identity of whatever
// is already holding the machine.
//
// "another LANcast server is already running" was true and unactionable: it is
// the same sentence whether the holder is the installed service (which comes
// back by itself after a reboot, since it is delayed-auto-start), a stray
// desktop launch, or a build being tested from a terminal — and those want
// three different responses. Finding out which took a Get-CimInstance and an
// sc.exe query. The guard knows how to ask; it just was not asking.
//
// Every failure to identify the holder degrades to the old message rather than
// printing half a sentence: the point is to say more when more is known, never
// to assert something unverified about the operator's machine.
func logAlreadyRunning(log *slog.Logger) {
	const msg = "another LANcast server is already running"
	running, ok := service.RunningServer()
	if !ok {
		log.Error(msg, "hint", service.Running{}.Hint())
		return
	}
	log.Error(msg, "holder", running.Describe(), "hint", running.Hint())
}

// newLogger builds the stderr logger used foreground and by the service run mode.
func newLogger(verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

// shutdownGrace is how long in-flight requests get to finish once a stop has
// been asked for. It is deliberately short: the service control manager judges
// a service by whether it stops when told, and a media server always has a
// long-lived connection somewhere — a stream, or the keep-alive every browser
// tab leaves behind. Anything still running when it expires is closed rather
// than waited on.
const shutdownGrace = 5 * time.Second

// run boots and serves until ctx is cancelled. The caller owns the shutdown
// signal: interactive mode passes a Ctrl-C/SIGTERM context, and the Windows
// service handler passes one it cancels when the SCM asks it to stop — so the
// same server runs identically foreground or as a service (ADR 0016).
func run(ctx context.Context, addr, dataDir string, log *slog.Logger) error {
	cfg, err := config.Resolve(addr, dataDir)
	if err != nil {
		return err
	}

	settings, err := config.LoadSettings(cfg.DataDir)
	if err != nil {
		return err
	}

	// Put ffmpeg/ffprobe on this process's PATH before anything looks for them.
	// Under a service the account's PATH does not include a per-user install, and
	// the failure is silent: nothing probes, every playback decision falls back to
	// direct play, and undecodable files reach the browser (ADR 0016).
	toolDir, toolsOK := mediatools.Resolve(settings.Get().FFmpegDir)
	if toolsOK {
		log.Info("media tools found", "location", mediatools.Describe(toolDir, true))
		// Remember where they were so the next start does not have to search,
		// which matters most for a service that cannot see the user's PATH.
		if cur := settings.Get(); cur.FFmpegDir != toolDir && toolDir != "" {
			cur.FFmpegDir = toolDir
			if err := settings.Set(cur); err != nil {
				log.Warn("could not record the media tools location", "error", err)
			}
		}
	} else {
		log.Warn("ffmpeg/ffprobe not found — files will be direct-played only",
			"hint", "install ffmpeg, or set its directory in Settings; without it LANcast cannot inspect or convert media")
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
	pluginsRoot := filepath.Join(cfg.DataDir, "plugins")

	// The registry is rebuilt whenever settings change (a newly entered key) or
	// plugins change (install/grant/enable/remove). `plugins` is captured by both
	// closures, so reassigning it and rebuilding picks up the new set.
	reg := meta.NewRegistry()
	var regMu sync.Mutex
	var plugins []*plugin.Plugin
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

	reloadPlugins := func() error {
		installed, err := st.ListInstalledPlugins(context.Background())
		if err != nil {
			return err
		}
		records := make([]plugin.InstalledRecord, 0, len(installed))
		for _, ip := range installed {
			records = append(records, plugin.InstalledRecord{
				Name: ip.Name, Digest: ip.Digest, Enabled: ip.Enabled,
				GrantedHTTP: ip.GrantedHTTP, GrantedSecrets: ip.GrantedSecrets,
			})
		}
		loaded := pluginRT.LoadInstalled(context.Background(), pluginsRoot, records, plugin.KnownBadDigests)
		regMu.Lock()
		plugins = loaded
		regMu.Unlock()
		rebuild(settings.Get())
		return nil
	}
	if err := reloadPlugins(); err != nil {
		return fmt.Errorf("load plugins: %w", err)
	}

	if settings.Get().TMDBKey == "" {
		// Not a warning: running without a key is a supported configuration,
		// and saying so plainly is what makes the no-phone-home promise real.
		log.Info("no metadata provider configured; using filename and NFO metadata only")
	}

	worker := newWorker(st, reg, art, settings, log)

	prober := probe.New()
	probes := probe.NewWorker(st, prober, log)

	// Album art comes off the disk, not from a provider: the picture embedded
	// in a track, or a cover.jpg beside it (ADR 0024). It gets its own worker
	// for the reason probing does — extraction spawns a process per album, and
	// a first scan should not wait on hundreds of them — and pointedly not a
	// hook on enrichment, which no music item ever reaches because ADR 0024
	// ships no music provider.
	covers := coverart.NewWorker(st, art, coverart.NewResolver(coverart.NewExtractor()), log)
	// A photo is its own artwork (ADR 0028), so a picture library's thumbnails
	// are generated rather than found. Its own worker for the reason the two
	// above are: decoding and resizing a library of photographs is minutes of
	// CPU, and a scan that did it inline would look like a hang on its first
	// run. ffmpeg is handed over for HEIC, which nothing in the standard
	// library can read and which a phone backup is mostly made of.
	photos := photo.NewWorker(st, art, &photo.Decoder{FFmpeg: photo.NewFFmpeg()}, log)
	// Music takes its metadata from the file's own tags during the scan, not
	// from the filename (ADR 0024). Without a prober the scan still works and
	// tracks keep what their folders gave them.
	scanner.SetTagReader(prober)

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

	// Background passes are tracked so shutdown can wait for them to stop
	// touching the database before it is closed. Cancelling only asks; a worker
	// mid-query keeps the file open until that query returns, and closing the
	// store underneath one is how a clean exit leaves a locked database behind.
	var workers sync.WaitGroup

	enrichSoon := func() {
		if !settings.Get().AutoEnrich {
			return
		}
		workers.Add(1)
		go func() {
			defer workers.Done()
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
		workers.Add(1)
		go func() {
			defer workers.Done()
			probeMu.Lock()
			defer probeMu.Unlock()
			if err := probes.Run(enrichCtx); err != nil && !errors.Is(err, context.Canceled) {
				log.Warn("probe pass failed", "error", err)
			}
		}()
	}

	var photoMu sync.Mutex
	photoSoon := func() {
		workers.Add(1)
		go func() {
			defer workers.Done()
			photoMu.Lock()
			defer photoMu.Unlock()
			if err := photos.Run(enrichCtx); err != nil && !errors.Is(err, context.Canceled) {
				log.Warn("photo thumbnail pass failed", "error", err)
			}
		}()
	}

	var coverMu sync.Mutex
	coverSoon := func() {
		workers.Add(1)
		go func() {
			defer workers.Done()
			coverMu.Lock()
			defer coverMu.Unlock()
			if err := covers.Run(enrichCtx); err != nil && !errors.Is(err, context.Canceled) {
				log.Warn("album art pass failed", "error", err)
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

	restartWidens := restartWidensBind(cfg.Addr, lanBound)

	switch {
	case userCount == 0 && restartWidens:
		log.Warn("no account set — listening on loopback only",
			"addr", listenAddr, "hint", "create an account in the browser, then restart to reach LANcast from other devices")
	case userCount == 0:
		log.Warn("no account set — listening on loopback only",
			"addr", listenAddr, "hint", "create an account in the browser to unlock the rest of the API")
	case !lanBound:
		// Secured, but the operator asked for a loopback address. Not a
		// warning — it is a deliberate configuration, and the only surprise
		// worth pre-empting is why there is no HTTPS.
		log.Info("listening on loopback only",
			"addr", listenAddr, "hint", "TLS stays off because nothing leaves this machine; bind to a LAN address to enable it")
	}

	// Asks the project's releases endpoint whether a newer version exists. The
	// setting gates whether it runs on a timer; the manual check in Settings
	// works either way, because someone who does not want a timer may still
	// want to ask once.
	// The previous executable, renamed aside by the last shutdown's swap. It
	// could not be deleted then because it was the running image; it can be
	// now, because this process is the new one.
	if exe, err := os.Executable(); err == nil {
		if n := selfupdate.CleanupOld(filepath.Dir(exe)); n > 0 {
			log.Info("removed the previous version", "files", n)
		}
	}

	updates := update.New(api.Version)
	if settings.Get().UpdateCheck {
		workers.Add(1)
		go func() {
			defer workers.Done()
			// Not on the first tick: a server starting up has more useful things
			// to do than talk to the internet, and nothing here is urgent.
			select {
			case <-time.After(2 * time.Minute):
			case <-enrichCtx.Done():
				return
			}
			for {
				if settings.Get().UpdateCheck && updates.Due() {
					updates.Check(enrichCtx)
				}
				select {
				case <-time.After(6 * time.Hour):
				case <-enrichCtx.Done():
					return
				}
			}
		}()
	}

	srv := &http.Server{
		Addr: listenAddr,
		Handler: api.New(api.Deps{
			LANBound: lanBound, RestartWidens: restartWidens,
			Store: st, Scanner: scanner, Registry: reg, Artwork: art,
			Worker: worker, Probes: probes, Covers: covers, Photos: photos, Trans: trans, Subs: subs,
			Settings: settings, DataDir: cfg.DataDir, Log: log, Web: web.Handler(),
			Updates: updates,
			Rebuild: func(s config.Settings) {
				rebuild(s)
				worker.SetNFOWriter(nfoWriterFor(s))
			},
			ReloadPlugins: reloadPlugins,
			Enrich:        enrichSoon,
			Probe:         probeSoon,
			Cover:         coverSoon,
		}).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// No write timeout: streaming a film is a legitimately long response.
	}

	// A scan produces pending items; both workers consume them. Without this
	// wiring a fresh scan stays unenriched and unprobed until the next restart.
	scanner.OnFinish(func() {
		probeSoon()
		enrichSoon()
		coverSoon()
		photoSoon()
	})

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
	waitForWorkers(&workers, log)

	// Stop accepting, and stop keeping connections alive, before asking for a
	// graceful close. Without this an idle keep-alive — every browser tab leaves
	// one — is a connection Shutdown politely waits on.
	srv.SetKeepAlivesEnabled(false)
	if redirectSrv != nil {
		redirectSrv.SetKeepAlivesEnabled(false)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if redirectSrv != nil {
		_ = redirectSrv.Shutdown(shutdownCtx)
	}

	// Graceful first, then forced. A stop that waits on an in-flight stream for
	// as long as the stream feels like taking is a stop that never completes,
	// and a service that does not complete its stop is killed and restarted —
	// which is worse than cutting one playback short. Closing is what makes
	// "closed" mean closed.
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Warn("graceful shutdown did not finish; closing connections",
			"grace", shutdownGrace, "error", err)
		if err := srv.Close(); err != nil {
			return err
		}
	}
	if redirectSrv != nil {
		_ = redirectSrv.Close()
	}
	// Last thing, once nothing is serving and the workers are done. The files
	// being replaced may include this process's own executable, which is why
	// the swap is a rename rather than an overwrite and why it happens here
	// rather than at startup — the next start runs the new version.
	applyStagedUpdate(cfg.DataDir, log)

	log.Info("stopped")
	return nil
}

// bindAddr forces an unsecured server onto loopback, preserving the requested
// port. It reports whether the result is reachable from the network.
func bindAddr(requested string, secured bool) (addr string, lanBound bool) {
	if secured {
		// The operator's address is honoured. Whether it reaches anyone else is
		// a separate question from whether a password is set, and conflating
		// the two is what made a server bound to 127.0.0.1 announce itself as
		// LAN-reachable and serve a certificate warning on localhost — the
		// exact friction ADR 0014 set out to avoid.
		return requested, !loopbackOnly(requested)
	}

	_, port, err := net.SplitHostPort(requested)
	if err != nil {
		// A bare port like ":8080" or something unparseable: fall back to a
		// known-safe address rather than guessing.
		return "127.0.0.1:8080", false
	}
	return net.JoinHostPort("127.0.0.1", port), false
}

// restartWidensBind reports whether restarting would bind wider than the
// server is bound right now.
//
// Two conditions, and both matter. The loopback restriction has to be what is
// holding it back — a server already reaching the network gains nothing from a
// restart — and the configured address has to actually reach further once that
// restriction lifts. An operator who set `-addr 127.0.0.1:8080` and is told
// "restart to reach LANcast from other devices" will restart, see no change,
// and have no way to tell whether the advice or their setup is wrong.
//
// requested is the *configured* address, not the resolved one: the resolved
// address of an unsecured server is always loopback, which is precisely the
// state this question is asked from.
func restartWidensBind(requested string, lanBound bool) bool {
	return !lanBound && !loopbackOnly(requested)
}

// loopbackOnly reports whether a listen address reaches only this machine.
//
// An empty host — ":8080" — means every interface, which is the opposite of
// loopback-only, so it is the case most worth getting right. Anything that
// cannot be parsed is treated as reachable: guessing "local" wrongly puts
// credentials on the wire in plaintext, while guessing "reachable" wrongly
// costs one certificate warning. Those are not the same mistake.
func loopbackOnly(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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

// waitForWorkers lets the background passes finish their current query.
//
// Cancelling a context only asks; a worker in the middle of a statement keeps
// the database open until it returns, and closing the store underneath one
// leaves the file locked after a shutdown that reported success. Bounded,
// because a worker that will not stop must not hold the whole exit — the point
// of this is that closing means closed, not that it means eventually.
func waitForWorkers(wg *sync.WaitGroup, log *slog.Logger) {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(workerStopGrace):
		log.Warn("background workers did not stop in time; closing anyway",
			"grace", workerStopGrace)
	}
}

// workerStopGrace bounds the wait for background passes. Short: they check for
// cancellation between items, so anything longer means one is genuinely stuck.
const workerStopGrace = 3 * time.Second

// applyStagedUpdate swaps in a verified update on the way down.
//
// Failure is logged and otherwise ignored: the server is already stopping, and
// an update that did not apply is a disappointment rather than a fault. The
// staged copy stays where it is and the next shutdown tries again.
func applyStagedUpdate(dataDir string, log *slog.Logger) {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	m, err := selfupdate.Apply(dataDir, filepath.Dir(exe))
	if errors.Is(err, os.ErrNotExist) {
		return // nothing staged, which is every ordinary shutdown
	}
	if err != nil {
		log.Error("could not apply the staged update; the install is unchanged",
			"version", m.Version, "error", err)
		return
	}
	log.Info("staged update applied; it takes effect on the next start",
		"version", m.Version, "files", len(m.Files))
}
