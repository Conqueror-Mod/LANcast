package peer_test

import (
	"crypto/tls"
	"crypto/x509"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lancast/internal/certpin"
	"lancast/internal/identity"
	"lancast/internal/peer"
	"lancast/internal/tlscert"
)

/*
 * Post-quantum key exchange (docs/security.md, "What is quantum-resistant").
 *
 * Since Go 1.24 a tls.Config that leaves CurvePreferences alone offers the
 * hybrid X25519MLKEM768 first, and a server that leaves it alone picks it
 * whenever the client offers it. LANcast gets that for nothing, on every
 * connection: browsers and the desktop window to the server, the native
 * players, and one paired server to another. It keeps getting it only for as
 * long as nobody writes a curve list, because a list written today is a list
 * that stays classical when the defaults move on. So these tests hold both
 * halves: no configuration names curves or turns ML-KEM off, and the
 * configurations the code builds really do negotiate the hybrid.
 *
 * What they cannot see is a browser's side. Chromium (and so WebView2, which
 * is the desktop window) offers X25519MLKEM768 by itself; that was checked by
 * hand and is recorded in security.md rather than here.
 */

// repoRoot is the module root, two levels up from internal/peer.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("no go.mod at %s: %v", root, err)
	}
	return root
}

/*
 * TestNoTLSConfigSetsCurvePreferences reads the source.
 *
 * Any mention of CurvePreferences in non-test code fails: a composite literal
 * key, an assignment to a field, anything. There is no allow-list on purpose.
 * Whoever needs one is choosing to fix the key exchange in place while the
 * library's defaults keep moving, and that should be argued in an ADR and this
 * test changed with it, not slipped in beside a handshake fix.
 *
 * The same goes for the GODEBUG switches that turn the hybrid off
 * (tlsmlkem=0, tlssecpmlkem=0), in a //go:debug line or go.mod's godebug
 * block. A //go:debug line in package main changes only that binary, which no
 * handshake test below would ever see.
 */
func TestNoTLSConfigSetsCurvePreferences(t *testing.T) {
	root := repoRoot(t)
	skipDirs := map[string]bool{"node_modules": true, ".git": true, ".claude": true, "third_party": true, "web": true}
	scanned := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if rel == "go.mod" {
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, bad := range []string{"tlsmlkem", "tlssecpmlkem"} {
				if strings.Contains(string(body), bad) {
					t.Errorf("go.mod mentions %s: the post-quantum key exchange must stay on", bad)
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			return nil
		}
		scanned++
		ast.Inspect(f, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id.Name == "CurvePreferences" {
				t.Errorf("%s: sets CurvePreferences. LANcast relies on Go's default curve list, "+
					"which offers the hybrid post-quantum X25519MLKEM768 (docs/security.md)",
					fset.Position(id.Pos()))
			}
			return true
		})
		for _, cg := range f.Comments {
			for _, c := range cg.List {
				if strings.HasPrefix(c.Text, "//go:debug") &&
					(strings.Contains(c.Text, "tlsmlkem") || strings.Contains(c.Text, "tlssecpmlkem")) {
					t.Errorf("%s: %s turns the post-quantum key exchange off", fset.Position(c.Pos()), c.Text)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// A walk that read nothing would pass for the wrong reason.
	if scanned < 500 {
		t.Fatalf("scanned only %d Go files; is the root %s right?", scanned, root)
	}
}

// handshake runs one TLS handshake over a pipe and returns what each side saw.
func handshake(t *testing.T, server, client *tls.Config) (srv, cli tls.ConnectionState) {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() { a.Close(); b.Close() })
	sc := tls.Server(a, server)
	cc := tls.Client(b, client)
	errc := make(chan error, 1)
	go func() { errc <- sc.Handshake() }()
	if err := cc.Handshake(); err != nil {
		t.Fatalf("client handshake: %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("server handshake: %v", err)
	}
	return sc.ConnectionState(), cc.ConnectionState()
}

func wantHybrid(t *testing.T, who string, cs tls.ConnectionState) {
	t.Helper()
	if cs.Version != tls.VersionTLS13 {
		t.Errorf("%s: negotiated %s, want TLS 1.3", who, tls.VersionName(cs.Version))
	}
	if cs.CurveID != tls.X25519MLKEM768 {
		t.Errorf("%s: key exchange %v, want X25519MLKEM768", who, cs.CurveID)
	}
	t.Logf("%s: %s, %v", who, tls.VersionName(cs.Version), cs.CurveID)
}

/*
 * browserFacing builds the listener's configuration the way serverapp does:
 * the generated certificate behind tlscert.Serving, TLS 1.2 as the floor, and
 * peer.Attach so a paired server's ClientHello is diverted to the peer config.
 * Kept to the same calls rather than a hand-made config, so a curve list added
 * to any of them is a curve list this sees.
 */
func browserFacing(t *testing.T, ident identity.Identity) (*tls.Config, tls.Certificate) {
	t.Helper()
	cert, err := tlscert.LoadOrGenerate(t.TempDir(), []string{"127.0.0.1", "localhost"})
	if err != nil {
		t.Fatal(err)
	}
	serving := tlscert.NewServing(cert, slog.New(slog.NewTextHandler(io.Discard, nil)))
	base := &tls.Config{GetCertificate: serving.GetCertificate, MinVersion: tls.VersionTLS12}
	cfg, err := peer.Attach(base, ident)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, cert
}

// A default Go client to the server: what a third-party client written in Go
// gets, and the stand-in for anything that leaves its own defaults alone.
func TestServerNegotiatesHybridWithDefaultClient(t *testing.T) {
	ident, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srvCfg, cert := browserFacing(t, ident)
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	srv, cli := handshake(t, srvCfg, &tls.Config{RootCAs: roots, ServerName: "localhost"})
	wantHybrid(t, "server", srv)
	wantHybrid(t, "default Go client", cli)
}

// The pinned client the desktop's native paths use (libmpv's relay, the retro
// player): certpin.TLSConfig, as built.
func TestServerNegotiatesHybridWithPinnedClient(t *testing.T) {
	ident, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srvCfg, cert := browserFacing(t, ident)
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	pin := certpin.SPKIFromDER(leaf.RawSubjectPublicKeyInfo)
	srv, cli := handshake(t, srvCfg, certpin.TLSConfig(pin))
	wantHybrid(t, "server", srv)
	wantHybrid(t, "certpin client", cli)
}

// One paired server dialling another: peer.ClientConfig against the listener
// with peer.Attach, the path every federation request takes.
func TestPeersNegotiateHybrid(t *testing.T) {
	host, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	guest, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srvCfg, _ := browserFacing(t, host)
	cliCfg, err := peer.ClientConfig(guest, host.Fingerprint())
	if err != nil {
		t.Fatal(err)
	}
	srv, cli := handshake(t, srvCfg, cliCfg)
	if len(srv.PeerCertificates) == 0 {
		t.Fatal("the peer config was not selected: no client certificate was presented")
	}
	wantHybrid(t, "host", srv)
	wantHybrid(t, "peer client", cli)
}
