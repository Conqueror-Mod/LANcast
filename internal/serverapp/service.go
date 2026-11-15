package serverapp

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"

	"lancast/internal/config"
	"lancast/internal/dataperm"
	"lancast/internal/mediatools"
	"lancast/internal/service"
)

// recordFFmpegDir persists the tools directory into the settings file in the
// service's pinned data dir, so the service reads it at startup instead of
// searching a PATH that will not contain it.
func recordFFmpegDir(dataDir, dir string) error {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	store, err := config.LoadSettings(dataDir)
	if err != nil {
		return err
	}
	cur := store.Get()
	cur.FFmpegDir = dir
	return store.Set(cur)
}

/*
 * serviceTools settles the ffmpeg folder to record for the service, while the
 * installing user's PATH can still be seen (ADR 0016).
 *
 * What it finds is recorded only if the service would run it: a folder that
 * Administrators and SYSTEM alone can change. A per-user install — winget's,
 * under AppData — is copied into the protected tools folder in the data
 * directory and that is recorded instead (R09-1). Run elevated, as install
 * always is; the copy is the administrator's decision to trust that ffmpeg.
 */
func serviceTools(dataDir, explicit string) (string, error) {
	found := explicit
	if found == "" {
		found = mediatools.Detect()
	}
	return mediatools.ForService(mediatools.ServiceGuard(), dataDir, found)
}

// runService handles `lancastd service <action> [flags]`. install/uninstall/
// start/stop/status delegate to the platform Manager; `run` is the internal mode
// the service host invokes (ADR 0016).
func runService(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: lancast service install|uninstall|start|stop|status|tools [-data DIR] [-addr ADDR]")
	}
	action := args[0]

	fs := flag.NewFlagSet("service", flag.ContinueOnError)
	data := fs.String("data", service.DefaultDataDir(runtime.GOOS),
		"machine-wide data directory (pinned; never the per-user default)")
	addr := fs.String("addr", ":8080", "listen address")
	ffmpegDir := fs.String("ffmpeg-dir", "",
		"directory containing ffmpeg, added to the service PATH")
	// verify and fallback are for restart after a port change (ADR 0089):
	// the new address must answer, or the old one is put back.
	verify := fs.String("verify", "", "restart: the address that must answer afterwards")
	fallback := fs.String("fallback", "", "restart: the address to go back to if it does not")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	// The internal run mode: the service host starts this; it is not something a
	// user invokes directly. On Linux systemd runs the plain binary instead, so
	// this path is exercised chiefly by the Windows SCM.
	if action == "run" {
		return serviceRun(*data, *addr)
	}

	mgr, err := service.NewManager()
	if err != nil {
		return err
	}
	switch action {
	case "install":
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("determine executable path: %w", err)
		}
		// The data directory first, so everything install and the service put
		// in it inherits SYSTEM and Administrators only (R10-2, R10-3). A
		// failure is a warning: the service re-asserts it at every start.
		if r, err := dataperm.Protect(*data); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not protect the data directory %s: %v\n", *data, err)
		} else if r.Applied {
			fmt.Printf("protected the data directory %s (SYSTEM and Administrators only)\n", *data)
		}
		// Record where ffmpeg lives while we can still see it. Install runs with
		// the installing user's PATH; the service will not have it, and without
		// this the service silently probes nothing (ADR 0016).
		tools, terr := serviceTools(*data, *ffmpegDir)
		if terr != nil {
			fmt.Fprintf(os.Stderr, "warning: ffmpeg was found but could not be copied where the service may run it: %v\n", terr)
		}
		if tools != "" {
			if err := recordFFmpegDir(*data, tools); err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not record the ffmpeg location: %v\n", err)
			}
		}

		cfg := service.Config{DataDir: *data, Addr: *addr, ExePath: exe, FFmpegDir: tools}
		if err := mgr.Install(cfg); err != nil {
			return err
		}
		fmt.Printf("installed %s, data dir %s\n", service.Name, *data)
		if tools == "" {
			fmt.Println("warning: ffmpeg/ffprobe were not found — LANcast will direct-play only until they are installed")
		} else {
			fmt.Printf("media tools: %s\n", tools)
		}
		return nil
	case "tools":
		// Copies a per-user ffmpeg into the protected tools folder and records
		// it, for a service that refused the folder it had (R09-1). Elevated,
		// like install; the service picks it up at its next start.
		tools, err := serviceTools(*data, *ffmpegDir)
		if err != nil {
			return err
		}
		if tools == "" {
			return fmt.Errorf("ffmpeg/ffprobe were not found; pass -ffmpeg-dir with the folder that holds them")
		}
		if err := recordFFmpegDir(*data, tools); err != nil {
			return err
		}
		fmt.Printf("media tools: %s\nrestart the service to use them: LANcast.exe service restart\n", tools)
		return nil
	case "uninstall":
		if err := mgr.Uninstall(); err != nil {
			return err
		}
		fmt.Printf("uninstalled %s\n", service.Name)
		return nil
	case "start":
		return mgr.Start()
	case "stop":
		return mgr.Stop()
	case "restart":
		// Spawned detached by the running server to finish an update: it stops
		// the service (which applies the staged files on the way down), waits
		// for the stop to complete, and starts it again. Run by hand it is
		// simply a restart.
		//
		// The process doing this cannot be the service itself — that is the
		// whole problem it solves — so it is a second, short-lived invocation
		// of the same binary. Renaming that binary underneath a running process
		// is permitted on Windows, which is what makes the swap safe while this
		// helper is still executing.
		if *verify != "" && *fallback != "" {
			return restartAndVerify(mgr, *data, *verify, *fallback)
		}
		return mgr.Restart(serviceRestartTimeout)
	case "status":
		s, err := mgr.Status()
		if err != nil {
			return err
		}
		fmt.Println(s)
		return nil
	default:
		return fmt.Errorf("unknown action %q (want install, uninstall, start, stop, restart, or status)", action)
	}
}

// serviceRestartTimeout bounds the wait for a stop before a restart gives up.
// Generous, because a stop that is finishing a scan or applying an update is
// doing real work — and the failure it prevents is worse than the wait: Start
// on a service still stopping fails, after the old version has already gone.
const serviceRestartTimeout = 90 * time.Second
