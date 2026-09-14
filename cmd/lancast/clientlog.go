package main

import (
	"log/slog"
	"os"

	"lancast/internal/applog"
)

/*
 * The window's own log.
 *
 * This binary is linked `-H=windowsgui` for release, so it has nowhere to
 * print: every slog line it has ever written went to a stderr nobody was
 * holding. The cost was not theoretical. Three separate faults in one evening —
 * an autostart setting that read as on, a client that could not start a server,
 * and a game window that was never moved — each had to be diagnosed by building
 * an instrumented copy of the client and running that instead of the installed
 * one, and twice the instrumented copy behaved differently enough to change the
 * answer.
 *
 * A log file is the cheaper instrument, and it is the one that exists when
 * somebody reports something a week later.
 *
 * Written to the client's own directory, never the server's: the server's may
 * belong to a service account this process cannot write to, which is the same
 * boundary that stops it starting a server there.
 */
func startLogging() {
	dir := clientDataDir()
	if dir == "" {
		// No profile directory means no log, and that is survivable: the app
		// still opens. Nothing here is worth refusing to start over.
		return
	}
	f, err := applog.OpenNamed(dir, applog.ClientFileName)
	if err != nil {
		return
	}
	/*
	 * Tee, so a build run from a terminal still prints.
	 *
	 * applog.Tee rather than io.MultiWriter for the reason the server gives
	 * where it does the same: the file is the one that has to succeed, and a
	 * closed or absent stderr must not take the log with it.
	 */
	slog.SetDefault(slog.New(slog.NewTextHandler(applog.Tee(f, os.Stderr), nil)))
	slog.Info("client started", "version", Version, "browser_mode", browserMode)
}
