package mpv

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

/*
 * The loopback relay between mpv and the server (ADR 0068).
 *
 * mpv cannot pin the server's self-signed certificate and cannot carry the
 * session, so it never talks to the server. It reads
 * `http://127.0.0.1:<port>/v/<random>`, and this relay makes the real request:
 * TLS pinned to the same public key the window pins, with the stream ticket in
 * the Authorization header.
 *
 * What stays true across the hop:
 *
 *  - **mpv's URL holds no credential.** The path is a random capability that
 *    maps to (item, ticket) inside this process. It will appear in mpv's log
 *    file, which is exactly why the ticket must not.
 *  - **Loopback only.** The listener is bound to 127.0.0.1, never a wildcard,
 *    so nothing off the machine can reach it; the random path is what keeps
 *    other local processes from using it.
 *  - **Range passes through.** Seeking is a range request, and a relay that
 *    buffered or dropped Range would turn every seek into a re-download.
 *  - **Only the stream.** The relay builds the upstream URL itself from an item
 *    id; there is no way to ask it for any other server path.
 */

// Relay is one loopback listener serving any number of registered streams.
type Relay struct {
	upstream *url.URL // the server origin, e.g. https://192.168.1.10:8443
	client   *http.Client
	ln       net.Listener
	srv      *http.Server

	mu      sync.Mutex
	streams map[string]entry
}

type entry struct {
	itemID int64
	ticket string
}

// forwarded are the request headers that matter to a ranged read. Nothing
// else from mpv reaches the server — in particular not a User-Agent that
// would make the server's logs describe mpv rather than LANcast.
var forwardedRequest = []string{"Range", "If-Range", "If-Modified-Since", "If-None-Match"}

var forwardedResponse = []string{
	"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges",
	"Last-Modified", "ETag",
}

// NewRelay starts a relay to the server at origin. pin is the base64 SHA-256
// of the server's SubjectPublicKeyInfo (internal/certpin); it is required for
// https and refused for http, so the two can never be mixed up into an
// unpinned TLS connection.
func NewRelay(origin, pin string) (*Relay, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("mpv relay: bad server origin %q", origin)
	}
	tr := &http.Transport{
		Proxy:               nil, // never a system proxy: the server is on the LAN
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     90 * time.Second,
	}
	switch u.Scheme {
	case "https":
		if pin == "" {
			return nil, errors.New("mpv relay: https server with no certificate pin")
		}
		tr.TLSClientConfig = pinnedTLS(pin)
	case "http":
		if pin != "" {
			return nil, errors.New("mpv relay: a pin was given for a plain-http server")
		}
	default:
		return nil, fmt.Errorf("mpv relay: unsupported scheme %q", u.Scheme)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("mpv relay: listen: %w", err)
	}
	r := &Relay{
		upstream: &url.URL{Scheme: u.Scheme, Host: u.Host},
		client:   &http.Client{Transport: tr},
		ln:       ln,
		streams:  map[string]entry{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v/{key}", r.serve)
	r.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = r.srv.Serve(ln) }()
	return r, nil
}

// pinnedTLS verifies the server by public key and nothing else. Skipping the
// chain and hostname checks is replaced by the pin, not relaxed: a CA-issued
// certificate for the right name still fails it. The same reasoning as
// internal/certpin and peer.ClientConfig.
func pinnedTLS(pin string) *tls.Config {
	return &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true, // replaced by VerifyPeerCertificate below
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 {
				return errors.New("server presented no certificate")
			}
			cert, err := x509.ParseCertificate(raw[0])
			if err != nil {
				return err
			}
			sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
			got := base64.StdEncoding.EncodeToString(sum[:])
			if subtle.ConstantTimeCompare([]byte(got), []byte(pin)) != 1 {
				return errors.New("server certificate does not match the pinned key")
			}
			return nil
		},
	}
}

// Register makes an item's stream available to mpv and returns the loopback
// URL to open. The ticket stays inside this process.
func (r *Relay) Register(itemID int64, ticket string) (string, error) {
	if itemID <= 0 || ticket == "" {
		return "", errors.New("mpv relay: need an item and a ticket")
	}
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	key := base64.RawURLEncoding.EncodeToString(b[:])
	r.mu.Lock()
	r.streams[key] = entry{itemID: itemID, ticket: ticket}
	r.mu.Unlock()
	return "http://" + r.ln.Addr().String() + "/v/" + key, nil
}

// Forget removes every registration, as when playback stops.
func (r *Relay) Forget() {
	r.mu.Lock()
	r.streams = map[string]entry{}
	r.mu.Unlock()
}

// Close stops the listener.
func (r *Relay) Close() error { return r.srv.Close() }

func (r *Relay) serve(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.mu.Lock()
	e, ok := r.streams[req.PathValue("key")]
	r.mu.Unlock()
	if !ok {
		http.NotFound(w, req)
		return
	}

	up := *r.upstream
	up.Path = "/api/stream/" + strconv.FormatInt(e.itemID, 10)
	out, err := http.NewRequestWithContext(req.Context(), req.Method, up.String(), nil)
	if err != nil {
		http.Error(w, "bad upstream request", http.StatusInternalServerError)
		return
	}
	for _, h := range forwardedRequest {
		if v := req.Header.Get(h); v != "" {
			out.Header.Set(h, v)
		}
	}
	out.Header.Set("Authorization", "Ticket "+e.ticket)
	out.Header.Set("User-Agent", "LANcast-Client (mpv)")

	resp, err := r.client.Do(out)
	if err != nil {
		// No detail to mpv: its log is not the place for the reason a pin
		// failed.
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for _, h := range forwardedResponse {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if req.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, resp.Body)
}
