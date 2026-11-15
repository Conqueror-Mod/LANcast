package remote

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"lancast/internal/retro/saves"
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
	// failGets makes every save GET a 500, as a server mid-restart answers.
	failGets bool
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
			if f.failGets {
				http.Error(w, "restarting", http.StatusInternalServerError)
				return
			}
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
			// based_on, as putSave honours it.
			if base := r.URL.Query().Get("based_on"); base != "" {
				now := saves.NoSave
				if cur, ok := f.saves[slot]; ok {
					sum := sha256.Sum256(cur)
					now = hex.EncodeToString(sum[:])
				}
				if now != base {
					w.WriteHeader(http.StatusConflict)
					io.WriteString(w, `{"error":{"code":"conflict","message":"the save on the server changed since this one was loaded"}}`)
					return
				}
			}
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

/*
 * Save RAM is only ever sent over the save it started from (R12-4).
 *
 * A player that could not fetch the save, or that loaded one another machine
 * has since replaced, would otherwise send its RAM over the real one.
 */
func TestStoreSRAMFollowsWhatWasLoaded(t *testing.T) {
	f := newFake()
	f.saves["sram"] = []byte("real save")

	// Never fetched: refused here, before the server is asked.
	g := newGame(t, f)
	if err := g.StoreSRAM([]byte("blank")); !errors.Is(err, ErrSRAMNotLoaded) {
		t.Errorf("store with nothing loaded: %v", err)
	}
	// A fetch that failed is the same as none.
	f.mu.Lock()
	f.failGets = true
	f.mu.Unlock()
	if _, err := g.LoadSRAM(); err == nil {
		t.Fatal("a failing server returned a save")
	}
	if err := g.StoreSRAM([]byte("blank")); !errors.Is(err, ErrSRAMNotLoaded) {
		t.Errorf("store after a failed fetch: %v", err)
	}
	f.mu.Lock()
	f.failGets = false
	f.mu.Unlock()

	// Loaded, then replaced by another machine: the server refuses.
	if b, err := g.LoadSRAM(); err != nil || string(b) != "real save" {
		t.Fatalf("load: %q %v", b, err)
	}
	f.mu.Lock()
	f.saves["sram"] = []byte("saved on the laptop")
	f.mu.Unlock()
	if err := g.StoreSRAM([]byte("stale")); !errors.Is(err, saves.ErrConflict) {
		t.Errorf("store over a save that changed: %v", err)
	}
	f.mu.Lock()
	got := string(f.saves["sram"])
	f.mu.Unlock()
	if got != "saved on the laptop" {
		t.Errorf("server holds %q, want the laptop's save untouched", got)
	}

	// Loaded and stored in turn: each store is based on the one before.
	g2 := newGame(t, f)
	if _, err := g2.LoadSRAM(); err != nil {
		t.Fatal(err)
	}
	for _, b := range []string{"one", "two", "three"} {
		if err := g2.StoreSRAM([]byte(b)); err != nil {
			t.Fatalf("store %q: %v", b, err)
		}
	}
	// And an empty slot loaded as empty accepts the first save.
	delete(f.saves, "sram")
	g3 := newGame(t, f)
	if b, err := g3.LoadSRAM(); b != nil || err != nil {
		t.Fatalf("empty slot: %q %v", b, err)
	}
	if err := g3.StoreSRAM([]byte("first")); err != nil {
		t.Errorf("first save into an empty slot: %v", err)
	}
}
