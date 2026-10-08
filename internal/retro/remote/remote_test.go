package remote

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeServer answers the four routes the player uses, and records whether
// every request carried the ticket — and only in the header.
type fakeServer struct {
	mu      sync.Mutex
	files   map[string]string
	saves   map[string][]byte
	cores   map[string][2]string
	fetches map[string]int
	badAuth int
	inURL   int
}

func newFake() *fakeServer {
	return &fakeServer{
		files: map[string]string{"Disc.cue": "cue", "Disc (Track 1).bin": "track one"},
		saves: map[string][]byte{}, cores: map[string][2]string{}, fetches: map[string]int{},
	}
}

func (f *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Ticket secret" {
		f.badAuth++
		http.Error(w, "no", http.StatusUnauthorized)
		return
	}
	if strings.Contains(r.URL.RawQuery, "secret") {
		f.inURL++
	}
	switch {
	case r.URL.Path == "/api/items/7/files":
		json.NewEncoder(w).Encode(map[string]any{"files": []map[string]any{
			{"name": "Disc.cue", "size_bytes": 3, "present": true},
			{"name": "Disc (Track 1).bin", "size_bytes": 9, "present": true},
		}})
	case r.URL.Path == "/api/stream/7/files":
		name := r.URL.Query().Get("name")
		f.fetches[name]++
		body, ok := f.files[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, body)
	case strings.HasPrefix(r.URL.Path, "/api/items/7/saves/"):
		slot := strings.TrimPrefix(r.URL.Path, "/api/items/7/saves/")
		switch r.Method {
		case http.MethodGet:
			b, ok := f.saves[slot]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				io.WriteString(w, `{"error":{"code":"not_found","message":"no save in this slot"}}`)
				return
			}
			if c, ok := f.cores[slot]; ok {
				w.Header().Set("X-LANcast-Core", c[0])
				w.Header().Set("X-LANcast-Core-Version", c[1])
			}
			w.Write(b)
		case http.MethodPut:
			b, _ := io.ReadAll(r.Body)
			f.saves[slot] = b
			if c := r.URL.Query().Get("core"); c != "" {
				f.cores[slot] = [2]string{c, r.URL.Query().Get("core_version")}
			}
			io.WriteString(w, `{}`)
		}
	default:
		http.NotFound(w, r)
	}
}

func newGame(t *testing.T, f *fakeServer) *Game {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	g, err := New(srv.URL, "", 7, "secret")
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestDownloadFetchesOnceAndReturnsTheEntry(t *testing.T) {
	f := newFake()
	g := newGame(t, f)
	dir := t.TempDir()
	entry, err := g.Download(context.Background(), dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if entry != filepath.Join(dir, "Disc.cue") {
		t.Errorf("entry = %s", entry)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "Disc (Track 1).bin")); string(b) != "track one" {
		t.Errorf("track = %q", b)
	}
	// Playing it again costs nothing: the files are there at their sizes.
	if _, err := g.Download(context.Background(), dir, nil); err != nil {
		t.Fatal(err)
	}
	if f.fetches["Disc (Track 1).bin"] != 1 {
		t.Errorf("fetched %d times, want once", f.fetches["Disc (Track 1).bin"])
	}
	if f.badAuth != 0 || f.inURL != 0 {
		t.Errorf("ticket missing %d times, in a URL %d times", f.badAuth, f.inURL)
	}
}

// A name from the server never becomes a path outside the cache.
func TestLocalPathRefusesEscapes(t *testing.T) {
	dir := t.TempDir()
	for _, bad := range []string{"", "..", "../x", "..\\x", "a/../../x", "/etc/passwd", `C:\x`, `\\server\share\x`} {
		if p, err := LocalPath(dir, bad); err == nil {
			t.Errorf("%q became %s", bad, p)
		}
	}
	if p, err := LocalPath(dir, ".hidden/Disc 1.cue"); err != nil || p != filepath.Join(dir, ".hidden", "Disc 1.cue") {
		t.Errorf("a good name: %s %v", p, err)
	}
}

// The save store round-trips, a missing save RAM is no save rather than an
// error, and a state comes back with the core that wrote it.
func TestSaves(t *testing.T) {
	f := newFake()
	g := newGame(t, f)
	if b, err := g.LoadSRAM(); b != nil || err != nil {
		t.Errorf("no save yet: %v %v", b, err)
	}
	if err := g.StoreSRAM([]byte("battery")); err != nil {
		t.Fatal(err)
	}
	if b, _ := g.LoadSRAM(); string(b) != "battery" {
		t.Errorf("sram = %q", b)
	}
	if err := g.StoreState("state-3", []byte("dump"), "mgba", "0.10"); err != nil {
		t.Fatal(err)
	}
	b, core, ver, err := g.LoadState("state-3")
	if err != nil || string(b) != "dump" || core != "mgba" || ver != "0.10" {
		t.Errorf("state = %q %q %q %v", b, core, ver, err)
	}
	if _, _, _, err := g.LoadState("state-4"); err == nil {
		t.Error("an empty slot loaded")
	}
}

func TestNewRefusesMixedTLS(t *testing.T) {
	if _, err := New("https://192.168.1.2:8443", "", 7, "t"); err == nil {
		t.Error("https with no pin was accepted")
	}
	if _, err := New("http://127.0.0.1:8080", "pin", 7, "t"); err == nil {
		t.Error("a pin for plain http was accepted")
	}
	if _, err := New("http://127.0.0.1:8080", "", 0, "t"); err == nil {
		t.Error("no game was accepted")
	}
}
