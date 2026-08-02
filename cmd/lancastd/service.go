package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	"lancast/internal/service"
)

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
		cfg := service.Config{DataDir: *data, Addr: *addr, ExePath: exe, FFmpegDir: *ffmpegDir}
		if err := mgr.Install(cfg); err != nil {
			return err
		}
		fmt.Printf("installed %s, data dir %s\n", service.Name, *data)
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
	case "status":
		s, err := mgr.Status()
		if err != nil {
			return err
		}
		fmt.Println(s)
		return nil
	default:
		return fmt.Errorf("unknown action %q (want install, uninstall, start, stop, or status)", action)
	}
}
