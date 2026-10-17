package api

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

/*
 * Changing the port, from Server › Network (ADR 0089).
 *
 * Only the port. The host part of the address, and the loopback rule that
 * narrows it to 127.0.0.1 while no password is set, are untouched: a restarted
 * server binds exactly as it would have, on a different number.
 *
 * The change restarts the server. The new port is tried first -- a quick
 * listen-and-close on the address the server would bind -- so the common
 * mistake, a port something else holds, is a 409 here rather than a restart
 * and a rollback. What the probe cannot see (a port taken in the seconds
 * between), the restart helper catches: the old port goes back if the new one
 * does not answer.
 */

// PortChanger changes the port the installed service listens on: it rewrites
// the service and starts a restart that rolls back if the new port does not
// answer. Nil when this server is not the installed Windows service.
type PortChanger interface {
	Change(port string) error
}

// SetPortChanger hands the server its port changer.
func (s *Server) SetPortChanger(p PortChanger) { s.portChanger = p }

// PortChange is how the last port change went, written by the restart helper
// to the data folder because the process that was asked is gone by then.
type PortChange struct {
	From  string `json:"from"`
	To    string `json:"to"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	At    int64  `json:"at"`
}

const portChangeFile = "port-change.json"

// WritePortChange records a change's outcome.
func WritePortChange(dataDir string, r PortChange) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dataDir, portChangeFile), b, 0o600)
}

// lastPortChange is the last change's outcome while it is news: a day.
func (s *Server) lastPortChange() *PortChange {
	b, err := os.ReadFile(filepath.Join(s.dataDir, portChangeFile))
	if err != nil {
		return nil
	}
	var r PortChange
	if json.Unmarshal(b, &r) != nil || time.Since(time.Unix(r.At, 0)) > 24*time.Hour {
		return nil
	}
	return &r
}

// Ports the main listener may not take: the public certificate's second
// listener and DLNA's. Below 1024 is refused too: privileged on Linux, and on
// Windows the territory of every other web server.
var reservedPorts = map[int]string{
	443:  "443 is the public certificate's port",
	8200: "8200 is DLNA's port",
}

// portChangeBlocker is why the port cannot be changed from here, or "".
func (s *Server) portChangeBlocker() string {
	if s.portChanger == nil {
		return "this server was not started as the installed service, so its port is the -addr it was started with"
	}
	return ""
}

func (s *Server) putPort(w http.ResponseWriter, r *http.Request) {
	if why := s.portChangeBlocker(); why != "" {
		writeError(w, http.StatusConflict, "conflict", why)
		return
	}
	var req struct {
		Port int `json:"port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	// The named reason first: 443 is also below 1024, and "it is the public
	// certificate's" is the more useful thing to be told.
	if why, ok := reservedPorts[req.Port]; ok {
		writeError(w, http.StatusBadRequest, "bad_request", why+", so the server cannot use it too")
		return
	}
	if req.Port < 1024 || req.Port > 65535 {
		writeError(w, http.StatusBadRequest, "bad_request", "the port must be from 1024 to 65535")
		return
	}
	host, cur, _ := net.SplitHostPort(s.listenAddr)
	port := strconv.Itoa(req.Port)
	if port == cur {
		writeError(w, http.StatusBadRequest, "bad_request", "the server already listens on port "+port)
		return
	}

	// Tried on the address the server will actually bind, so the probe never
	// opens anything wider than the loopback rule allows.
	ln, err := net.Listen("tcp", net.JoinHostPort(host, port))
	if err != nil {
		writeError(w, http.StatusConflict, "port_in_use",
			"port "+port+" is in use by something else on this computer")
		return
	}
	_ = ln.Close()

	if err := s.portChanger.Change(port); err != nil {
		s.writeInternal(w, err, "change the port")
		return
	}
	s.audit(r, "settings.port", "settings", "", "Moved the server from port "+cur+" to "+port,
		map[string]any{"from": cur, "to": port})
	writeJSON(w, http.StatusAccepted, map[string]any{"port": port, "restarting": true})
}
