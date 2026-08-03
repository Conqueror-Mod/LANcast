package main

import (
	"crypto/tls"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"lancast/internal/tlscert"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestSplitTLSRoutesBothSchemes stands up the demux on one port and confirms an
// HTTPS client reaches the real handler while a plaintext client is redirected —
// the whole point of ADR 0014's single-port promise.
func TestSplitTLSRoutesBothSchemes(t *testing.T) {
	cert, err := tlscert.LoadOrGenerate(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("cert: %v", err)
	}
	tlsConfig := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}

	base, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer base.Close()

	tlsLn, plainLn := splitTLS(base, discardLogger())

	appSrv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "app-ok")
	})}
	redirectSrv := &http.Server{Handler: http.HandlerFunc(httpsRedirect)}
	go appSrv.Serve(tls.NewListener(tlsLn, tlsConfig))
	go redirectSrv.Serve(plainLn)
	defer appSrv.Close()
	defer redirectSrv.Close()

	addr := base.Addr().String()

	// HTTPS reaches the app handler.
	httpsClient := &http.Client{
		Timeout:   3 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
	resp, err := httpsClient.Get("https://" + addr + "/home")
	if err != nil {
		t.Fatalf("https get: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "app-ok" {
		t.Errorf("https body = %q, want app-ok", body)
	}

	// Plaintext HTTP is redirected to the https scheme on the same host:port,
	// rather than failing a TLS handshake.
	noRedirect := &http.Client{
		Timeout:       3 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err = noRedirect.Get("http://" + addr + "/home?x=1")
	if err != nil {
		t.Fatalf("http get: %v", err)
	}
	defer resp.Body.Close()
	// Temporary, never permanent. A 301 is cached by browsers indefinitely, so
	// one visit while TLS was on would leave the browser refusing plain HTTP at
	// this host and port forever — and the scheme is not permanent: clearing
	// the accounts puts the same address back on plaintext.
	if resp.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("http status = %d, want 307", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusMovedPermanently {
		t.Fatal("permanent redirect: a browser will cache this and never retry HTTP")
	}
	if loc := resp.Header.Get("Location"); loc != "https://"+addr+"/home?x=1" {
		t.Errorf("redirect Location = %q, want https://%s/home?x=1", loc, addr)
	}
}
