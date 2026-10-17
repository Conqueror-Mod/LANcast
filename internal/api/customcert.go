package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"lancast/internal/tlscert"
)

/*
 * A certificate of the operator's own (ADR 0088), in Server › Network.
 *
 * Two paths to PEM files, as the settings file always had them, so a renewal
 * tool that rewrites those files keeps working with no re-upload: the server
 * re-reads them when they change. Saving checks the pair the way a browser
 * will -- readable, a pair, in date -- so a mistake is heard while the admin
 * is still looking at the form, and a pair that later stops loading is served
 * around rather than stopping the server.
 *
 * Admin only and audited. The key's contents never leave the server: these
 * routes take and return paths, and describe only the certificate.
 */

// ServingCert is what the custom-certificate routes need from
// tlscert.Serving.
type ServingCert interface {
	Configure(certFile, keyFile string)
	Status() tlscert.ServingStatus
}

// SetServingCert hands the server the certificate it serves the network.
// Nil (a loopback-only server, which serves no TLS) leaves the routes
// reporting the feature as unavailable.
func (s *Server) SetServingCert(c ServingCert) { s.serving = c }

type customCertStatus struct {
	// Available is false on a loopback-only server: there is no HTTPS to put
	// a certificate on.
	Available bool `json:"available"`
	tlscert.ServingStatus
}

func (s *Server) getCustomCert(w http.ResponseWriter, r *http.Request) {
	out := customCertStatus{}
	if s.serving != nil {
		out = customCertStatus{Available: true, ServingStatus: s.serving.Status()}
	} else {
		cur := s.settings.Get()
		out.CertFile, out.KeyFile, out.Serving = cur.TLSCertFile, cur.TLSKeyFile, "none"
	}
	if out.Names == nil {
		out.Names = []string{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) putCustomCert(w http.ResponseWriter, r *http.Request) {
	if s.serving == nil {
		writeError(w, http.StatusConflict, "conflict",
			"this server is loopback-only, so it serves no HTTPS to put a certificate on")
		return
	}
	var req struct {
		CertFile string `json:"cert_file"`
		KeyFile  string `json:"key_file"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	certFile, keyFile := cleanPath(req.CertFile), cleanPath(req.KeyFile)
	if certFile == "" || keyFile == "" {
		writeError(w, http.StatusBadRequest, "bad_request",
			"both cert_file and key_file are needed; DELETE goes back to LANcast's own certificate")
		return
	}
	if _, err := tlscert.LoadCustom(certFile, keyFile, time.Now()); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	s.setCustomCert(w, r, certFile, keyFile)
}

func (s *Server) deleteCustomCert(w http.ResponseWriter, r *http.Request) {
	if s.serving == nil {
		writeError(w, http.StatusConflict, "conflict",
			"this server is loopback-only, so it serves no HTTPS to put a certificate on")
		return
	}
	s.setCustomCert(w, r, "", "")
}

func (s *Server) setCustomCert(w http.ResponseWriter, r *http.Request, certFile, keyFile string) {
	prev := s.settings.Get()
	next := prev
	next.TLSCertFile, next.TLSKeyFile = certFile, keyFile
	if err := s.settings.Set(next); err != nil {
		s.writeInternal(w, err, "save settings")
		return
	}
	s.serving.Configure(certFile, keyFile)
	if prev.TLSCertFile != certFile || prev.TLSKeyFile != keyFile {
		summary := "Went back to LANcast's own certificate"
		if certFile != "" {
			summary = "Serving the certificate in " + certFile
		}
		s.audit(r, "settings.custom_certificate", "settings", "", summary,
			map[string]any{"cert_file": certFile, "key_file": keyFile})
	}
	s.getCustomCert(w, r)
}

// cleanPath takes a path as people paste it: Windows' "Copy as path" wraps
// it in double quotes.
func cleanPath(p string) string {
	p = strings.TrimSpace(p)
	if len(p) >= 2 && p[0] == '"' && p[len(p)-1] == '"' {
		p = strings.TrimSpace(p[1 : len(p)-1])
	}
	return p
}
