package serverapp

import (
	"context"
	"crypto/sha256"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"lancast/internal/api"
	"lancast/internal/config"
	"lancast/internal/dlna"
	"lancast/internal/identity"
	"lancast/internal/store"
	"lancast/internal/tlscert"
)

/*
 * DLNA's supervisor (ADR 0086): starts and stops the plain-HTTP listener and
 * discovery as Settings changes, so turning it on or off needs no restart.
 *
 * It runs only when every condition holds, and reports which one does not:
 * switched on, an account chosen that exists, and a server bound to the LAN —
 * which is to say a password is set. The loopback rule decides the last one;
 * this never widens it.
 */

// dlnaPort is where TVs fetch from. 8200 is what home media servers use.
const dlnaPort = 8200

type dlnaSupervisor struct {
	mu      sync.Mutex
	running bool
	reason  string
}

func (d *dlnaSupervisor) set(running bool, reason string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.running, d.reason = running, reason
}

// Status is what Server › DLNA shows.
func (d *dlnaSupervisor) Status() api.DLNAStatus {
	d.mu.Lock()
	defer d.mu.Unlock()
	return api.DLNAStatus{Running: d.running, Reason: d.reason, Port: dlnaPort}
}

// dlnaUUID is the device's UDN, from the server's identity: the same server is
// always the same device to a TV that remembers it.
func dlnaUUID(ident identity.Identity) string {
	sum := sha256.Sum256([]byte("lancast-dlna:" + ident.Fingerprint()))
	var b [16]byte
	copy(b[:], sum[:16])
	return dlna.UUIDFrom(b)
}

func runDLNA(ctx context.Context, sup *dlnaSupervisor, apiSrv *api.Server, st *store.Store,
	settings *config.SettingsStore, ident identity.Identity, lanBound bool, log *slog.Logger) {

	uuid := dlnaUUID(ident)
	server := "Windows/10 UPnP/1.0 LANcast/1"
	cfg := func() api.DLNAConfig {
		cur := settings.Get()
		name := cur.DLNAName
		if name == "" {
			name = "LANcast"
		}
		return api.DLNAConfig{UUID: uuid, Name: name, Account: cur.DLNAAccount}
	}
	preferredIP := func() string {
		for _, ip := range api.HomeFirst(tlscert.StableIPs()) {
			if p := net.ParseIP(ip); p != nil && p.To4() != nil {
				return ip
			}
		}
		return ""
	}

	var (
		srv    *http.Server
		cancel context.CancelFunc
	)
	stop := func() {
		if cancel != nil {
			cancel() // sends ssdp:byebye on the way out
			cancel = nil
		}
		if srv != nil {
			sctx, done := context.WithTimeout(context.Background(), 3*time.Second)
			_ = srv.Shutdown(sctx)
			done()
			srv = nil
			log.Info("dlna stopped")
		}
	}
	defer stop()

	why := func() string {
		cur := settings.Get()
		switch {
		case !cur.DLNAEnabled:
			return "off"
		case cur.DLNAAccount == "":
			return "no account chosen"
		case !lanBound:
			return "this server only answers this computer (no password, or a restart owed)"
		}
		if _, err := st.UserByID(ctx, cur.DLNAAccount); err != nil {
			return "the chosen account no longer exists"
		}
		return ""
	}

	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for {
		reason := why()
		switch {
		case reason != "" && srv != nil:
			stop()
			sup.set(false, reason)
		case reason != "":
			sup.set(false, reason)
		case srv == nil:
			ln, err := net.Listen("tcp", ":"+strconv.Itoa(dlnaPort))
			if err != nil {
				sup.set(false, "port "+strconv.Itoa(dlnaPort)+" is in use by another program")
				log.Warn("dlna: listen", "error", err)
				break
			}
			srv = &http.Server{Handler: apiSrv.DLNAHandler(cfg), ReadHeaderTimeout: 10 * time.Second}
			go func(s *http.Server) {
				if err := s.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
					log.Warn("dlna: serve", "error", err)
				}
			}(srv)
			// Kept in cancel, which stop() calls: that is what sends the
			// byebye when DLNA is switched off or the server stops.
			actx, c := context.WithCancel(ctx)
			cancel = c
			go func() {
				dev := dlna.Device{UUID: uuid, Server: server}
				if err := dlna.Announce(actx, dev, dlnaPort, preferredIP, log); err != nil {
					log.Warn("dlna: discovery", "error", err)
				}
			}()
			sup.set(true, "")
			log.Info("dlna started", "port", dlnaPort, "account", settings.Get().DLNAAccount, "address", preferredIP())
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
