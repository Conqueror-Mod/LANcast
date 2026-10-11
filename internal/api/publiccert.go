package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"lancast/internal/config"
	"lancast/internal/publiccert"
)

// PublicCertManager is what the public-certificate routes need from
// publiccert.Manager (ADR 0081).
type PublicCertManager interface {
	Configure(publiccert.Config)
	Status() publiccert.Status
}

// SetPublicCert hands the server its public-certificate manager.
//
// A setter rather than a Deps field because the manager is built where TLS is,
// which is after the API: it needs the serving certificate. Nil (a
// loopback-only server, which serves no TLS at all) leaves the routes
// reporting the feature as unavailable rather than failing.
func (s *Server) SetPublicCert(m PublicCertManager) { s.publicCert = m }

// PublicCertConfig turns stored settings into what the manager acts on. The
// directory override comes only from the settings file (ADR 0081 §1).
func PublicCertConfig(st config.Settings) publiccert.Config {
	pc := st.PublicCert
	return publiccert.Config{
		Enabled:      pc.Enabled,
		Hostname:     pc.Hostname,
		CA:           pc.CA,
		Email:        pc.Email,
		DirectoryURL: st.ACMEDirectoryURL,
	}
}

// getPublicCert reports the opt-in, the choices it may be set to, and what
// the certificate is doing.
func (s *Server) getPublicCert(w http.ResponseWriter, r *http.Request) {
	pc := s.settings.Get().PublicCert
	status := publiccert.Status{State: publiccert.StateOff}
	if s.publicCert != nil {
		status = s.publicCert.Status()
	}
	body := map[string]any{
		// False on a loopback-only server: there is no TLS to put a public
		// certificate on, and saying so beats a switch that does nothing.
		"available": s.publicCert != nil,
		"enabled":   pc.Enabled,
		"hostname":  pc.Hostname,
		"ca":        pc.CA,
		"email":     pc.Email,
		"cas":       publiccert.KnownCAs,
		"status":    status,
	}
	if pc.AgreedAt != 0 {
		body["agreed_at"] = time.Unix(pc.AgreedAt, 0).UTC()
	}
	writeJSON(w, http.StatusOK, body)
}

/*
 * putPublicCert turns the public certificate on, off, or at a new name.
 *
 * Saving the same values again is how an admin says "try now": it resets the
 * backoff (ADR 0081 §5). That is deliberate, and it is why this is a PUT of
 * the whole choice rather than a set of separate switches.
 *
 * Turning it on needs the CA's terms agreed in this request, unless they were
 * already agreed for the same CA. LANcast does not agree to terms on anybody's
 * behalf, and a CA changed since is a different agreement.
 */
func (s *Server) putPublicCert(w http.ResponseWriter, r *http.Request) {
	if s.publicCert == nil {
		writeError(w, http.StatusConflict, "conflict",
			"this server is loopback-only, so it serves no HTTPS to put a public certificate on")
		return
	}
	var req struct {
		Enabled    bool   `json:"enabled"`
		Hostname   string `json:"hostname"`
		CA         string `json:"ca"`
		Email      string `json:"email"`
		AgreeTerms bool   `json:"agree_terms"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}

	prev := s.settings.Get()
	next := prev
	pc := prev.PublicCert

	if req.Enabled {
		host, err := publiccert.NormalizeHostname(req.Hostname)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		if _, ok := publiccert.LookupCA(req.CA); !ok {
			writeError(w, http.StatusBadRequest, "bad_request",
				"ca must be one of the authorities reported by GET /api/settings/public-certificate")
			return
		}
		email := strings.TrimSpace(req.Email)
		if email != "" && (strings.Count(email, "@") != 1 || strings.ContainsAny(email, " \t:<>,;") ||
			strings.HasPrefix(email, "@") || strings.HasSuffix(email, "@")) {
			writeError(w, http.StatusBadRequest, "bad_request",
				"email must be an address like you@example.net, or empty")
			return
		}
		alreadyAgreed := pc.Enabled && pc.AgreedAt != 0 && pc.CA == req.CA
		if !req.AgreeTerms && !alreadyAgreed {
			writeError(w, http.StatusBadRequest, "bad_request",
				"the certificate authority's terms must be agreed to before it is asked for a certificate")
			return
		}
		if req.AgreeTerms && !alreadyAgreed {
			pc.AgreedAt = time.Now().Unix()
		}
		pc.Enabled, pc.Hostname, pc.CA, pc.Email = true, host, req.CA, email
	} else {
		// The name and CA are kept so turning it back on is one click; the
		// agreement is not, because turning it on again is asking again.
		pc.Enabled = false
		pc.AgreedAt = 0
	}
	next.PublicCert = pc

	if err := s.settings.Set(next); err != nil {
		s.writeInternal(w, err, "save settings")
		return
	}
	if prev.PublicCert != next.PublicCert {
		summary := "Turned the public certificate off"
		if pc.Enabled {
			summary = "Public certificate on for " + pc.Hostname + " from " + pc.CA
		}
		s.audit(r, "settings.public_certificate", "settings", "", summary,
			map[string]any{"enabled": pc.Enabled, "hostname": pc.Hostname, "ca": pc.CA})
	}
	s.publicCert.Configure(PublicCertConfig(next))
	s.getPublicCert(w, r)
}
