//go:build windows

package serverapp

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

/*
 * trayRun hands the desktop to the client (ADR 0080).
 *
 * There is one tray now, and it lives in the client's process beside the
 * window it hides into. This executable's own tray — a separate process
 * controlling the service, which needed a named event to tell the window to
 * quit and a second one to say it was alive — is gone.
 *
 * The entry points that used to start it still exist on real machines and
 * must keep working: the installer's "LANcast Server" shortcut and finish
 * page run `LANcast-Server.exe tray`, an older "LANcast Tray" run key does the
 * same at login, and somebody may double-click this file. Each is handed to
 * the client — hidden for `tray`, which asked for an icon; shown for a bare
 * double-click, which asked to see LANcast — and this process exits. The
 * client finds or waits for the server exactly as it does from its own
 * shortcut, and a second client launch raises the one already running, so
 * none of these can produce a second icon.
 */
func trayRun(addr, dataDir string, hidden bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	client := handoverTarget(exe)
	if _, err := os.Stat(client); err != nil {
		// Said out loud: the alternative is a server running with no window
		// and no icon, which is the invisible process LANcast promises never
		// to leave behind.
		alert("LANcast", "LANcast-Client.exe is not beside "+filepath.Base(exe)+
			", so there is no LANcast window or tray to open.\n\nReinstall LANcast to repair it.")
		return nil
	}
	return exec.Command(client, clientArgs(addr, dataDir, hidden)...).Start()
}

/*
 * handoverTarget is the program that runs the desktop.
 *
 * In the single LANcast executable (ADR 0080) that is this program again —
 * `LANcast tray` starts itself hidden. Only a leftover LANcast-Server.exe,
 * from before the two programs became one, hands to the client beside it.
 */
func handoverTarget(exe string) string {
	if strings.EqualFold(filepath.Base(exe), "LANcast-Server.exe") {
		return filepath.Join(filepath.Dir(exe), "LANcast-Client.exe")
	}
	return exe
}

// clientArgs carries this launch's choices over to the client: start hidden
// or not, and a non-default address or data directory.
func clientArgs(addr, dataDir string, hidden bool) []string {
	var args []string
	if hidden {
		args = append(args, "-hidden")
	}
	if addr != "" && addr != ":8080" {
		args = append(args, "-addr", addr)
	}
	if dataDir != "" {
		args = append(args, "-data", dataDir)
	}
	return args
}
