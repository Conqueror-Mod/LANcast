package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"

	"lancast/internal/config"
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

// runService handles `lancastd service <action> [flags]`. install/uninstall/
// start/stop/status delegate to the platform Manager; `run` is the internal mode
// the service host invokes (ADR 0016).
func runService(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: lancastd service install|uninstall|start|stop|status [-data DIR] [-addr ADDR]")
	}
	action := args[0]

	fs := flag.NewFlagSet("service", flag.ContinueOnError)
	data := fs.String("data", service.DefaultDataDir(runtime.GOOS),
		"machine-wide data directory (pinned; never the per-user default)")
	addr := fs.String("addr", ":8080", "listen address")
	ffmpegDir := fs.String("ffmpeg-dir", "",
		"directory containing ffmpeg, added to the service PATH")
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
		// Record where ffmpeg lives while we can still see it. Install runs with
		// the installing user's PATH; the service will not have it, and without
		// this the service silently probes nothing (ADR 0016).
		tools := *ffmpegDir
		if tools == "" {
			tools = mediatools.Detect()
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
