package api

import (
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
	if !s.serviceManaged {
		// Not a service: this process belongs to whoever started it, and the
		// honest answer is to say so rather than to kill it and hope something
		// brings it back.
		writeError(w, http.StatusPreconditionFailed, "not_a_service",
			"this server is not running as a service — close LANcast and open it again to finish the update")
		return
	}

	exe, err := os.Executable()
	if err != nil {
		s.writeInternal(w, err, "locate executable")
		return
	}

	cmd := exec.Command(exe, "service", "restart")
	cmd.Dir = filepath.Dir(exe)
	childproc.Hide(cmd)
	childproc.Detach(cmd)
	if err := cmd.Start(); err != nil {
		s.writeInternal(w, err, "start the restart helper")
		return
	}
	// Not waited on, deliberately: its first act is to stop this process. A
	// server that waited for its own killer would hold the request open until
	// the connection died, and the caller would read a dropped connection as a
	// failure rather than as the restart it asked for.
	s.audit(r, "update.restart", "", m.Version, "Restarted to finish updating to "+m.Version, nil)
	w.WriteHeader(http.StatusAccepted)
}
