package serverapp

import "flag"

// runTray parses the flags for the tray subcommand and hands off to the
// platform trayRun. hidden is true for `tray` itself (a shortcut, the old run
// key) and false for a bare double-click, which asked to see LANcast.
func runTray(args []string, hidden bool) error {
	fs := flag.NewFlagSet("tray", flag.ContinueOnError)
	addr := fs.String("addr", ":8080", "listen address")
	data := fs.String("data", "", "data directory (default: per-user config dir)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return trayRun(*addr, *data, hidden)
}
