package api

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"lancast/internal/childproc"
	"lancast/internal/selfupdate"
)

// restartForUpdate finishes a staged update by restarting the service.
//
// This exists because a staged update had nowhere to go. LANcast applies one on
// the way down, and when it runs as a Windows service nothing ever takes it
// down: the panel said "it takes effect the next time the server starts", which
// for a service means never. The only route through was an elevated
// Stop-Service, which applied the update and then left the machine with LANcast
// not running at all — an install that looked broken as the reward for updating.
//
// A service cannot restart itself, so this spawns a detached helper — the same
// binary, `service restart` — which stops the service, waits for the stop to
// actually complete, and starts it again. Renaming a running executable is
// permitted on Windows, which is what lets the helper keep executing while the
// swap replaces the file it was started from.
//
// Deliberately requires something staged. "Restart the server" is a bigger,
// more dangerous button than "finish the update" and would want its own
// thinking about sessions and in-flight playback; this one has a reason to
// interrupt them.
func (s *Server) restartForUpdate(w http.ResponseWriter, r *http.Request) {
	m, ok := selfupdate.Pending(s.dataDir)
	if !ok {
		writeError(w, http.StatusPreconditionFailed, "nothing_staged",
			"no update is staged, so there is nothing to restart for")
		return
	}
	if !s.canRestartSelf() {
		writeError(w, http.StatusPreconditionFailed, "not_a_service",
			"this server cannot restart itself here — close LANcast and open it again to finish the update")
		return
	}
	if err := s.restartSelf(); err != nil {
		s.writeInternal(w, err, "restart")
		return
	}
	s.audit(r, "update.restart", "", m.Version, "Restarted to finish updating to "+m.Version, nil)
	w.WriteHeader(http.StatusAccepted)
}

// canRestartSelf is whether restartSelf has a way to bring this server back.
func (s *Server) canRestartSelf() bool { return s.serviceManaged || s.relaunch != nil }

/*
 * restartSelf stops this server in a way that starts it again: through the
 * service manager when it is a service, or by the relaunch helper when it is
 * not. Shared by finishing an update and by restoring a backup, which both
 * need the next start rather than this process.
 *
 * Returns once the restart is under way. The caller answers its request
 * after: the helper's first act is to stop this process, and a graceful
 * shutdown lets the answer go out first.
 */
func (s *Server) restartSelf() error {
	if !s.serviceManaged {
		// Not a service. This used to be a dead end — a 412 telling the user to
		// close LANcast and open it again, with nothing afterwards to say
		// whether the swap had happened. The relaunch helper is the same idea
		// the service path uses, one level down: a detached copy waits for this
		// process to exit (which is when the staged files are applied, on the
		// way down) and starts it again with the arguments it had.
		if s.relaunch == nil {
			return errors.New("this server cannot restart itself here")
		}
		if err := s.relaunch(); err != nil {
			return fmt.Errorf("schedule the relaunch: %w", err)
		}
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate executable: %w", err)
	}

	cmd := exec.Command(exe, "service", "restart")
	cmd.Dir = filepath.Dir(exe)
	childproc.Hide(cmd)
	childproc.Detach(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start the restart helper: %w", err)
	}
	// Not waited on, deliberately: its first act is to stop this process. A
	// server that waited for its own killer would hold the request open until
	// the connection died, and the caller would read a dropped connection as a
	// failure rather than as the restart it asked for.
	return nil
}
