package main

import "flag"

// runTray parses the flags for the tray subcommand and hands off to the
// platform trayRun. The data dir defaults to the per-user config dir — tray mode
// is the interactive user, not a service, so the machine-wide pin does not apply.
func runTray(args []string) error {
	fs := flag.NewFlagSet("tray", flag.ContinueOnError)
	addr := fs.String("addr", ":8080", "listen address")
	data := fs.String("data", "", "data directory (default: per-user config dir)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return trayRun(*addr, *data)
}
