package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"lancast/internal/store"
)

/*
 * A game's files and saves (ADR 0073, stage 2), at the API — and the one
 * place a ticket may now write.
 */

type romFixture struct {
	h    *harness
	lib  *store.Library
	cart int64 // a single-file game
	disc int64 // a .cue and its two tracks
}

func newROMFixture(t *testing.T) romFixture {
	t.Helper()
	h := newHarness(t)
	dir := t.TempDir()
	lib, err := h.st.CreateLibrary(context.Background(), "Games", "retro", dir)
	if err != nil {
		t.Fatal(err)
	}
	write := func(rel, body string) string {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	upsert := func(path, platform string) int64 {
		id, err := h.st.UpsertItem(context.Background(), store.ScanFile{
			LibraryID: lib.ID, Path: path, Kind: "rom", Title: filepath.Base(path),
			SortTitle: filepath.Base(path), Platform: &platform,
			Container: filepath.Ext(path)[1:], SizeBytes: 1, MTime: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	cart := upsert(write("N64/Game.z64", "cartridge"), "n64")
	write("PS1/Disc/Disc (Track 1).bin", "track one")
	write("PS1/Disc/Disc (Track 2).bin", "track two")
	write("PS1/secret.txt", "not part of the game")
	disc := upsert(write("PS1/Disc/Disc.cue",
		"FILE \"Disc (Track 1).bin\" BINARY\nFILE \"Disc (Track 2).bin\" BINARY\nFILE \"..\\secret.txt\" BINARY\n"), "ps1")
	return romFixture{h: h, lib: lib, cart: cart, disc: disc}
}

// raw sends a request with the session cookie and this origin, carrying raw
// bytes — the shape of a save upload.
func (f romFixture) raw(t *testing.T, method, path string, body []byte, auth string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, f.h.srv.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	} else {
		req.Header.Set("Origin", f.h.srv.URL)
		if f.h.cookie != nil {
			req.AddCookie(f.h.cookie)
		}
	}
	resp, err := f.h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func body(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func TestGameFilesListTheDiscAndOnlyTheDisc(t *testing.T) {
	f := newROMFixture(t)
	var got struct {
		Files []RomFile `json:"files"`
	}
	decode(t, f.h.do(t, "GET", "/api/items/"+itoa(f.disc)+"/files", nil), &got)
	names := []string{}
	for _, x := range got.Files {
		names = append(names, x.Name)
		if !x.Present || x.SizeBytes == 0 {
			t.Errorf("%s not present", x.Name)
		}
	}
	want := []string{"Disc.cue", "Disc (Track 1).bin", "Disc (Track 2).bin"}
	if len(names) != len(want) {
		t.Fatalf("files = %v, want %v (the cue's ..\\secret.txt must not be listed)", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("files[%d] = %q, want %q", i, names[i], want[i])
		}
	}

	resp := f.h.do(t, "GET", "/api/stream/"+itoa(f.disc)+"/files?name="+url.QueryEscape("Disc (Track 2).bin"), nil)
	if resp.StatusCode != http.StatusOK || body(t, resp) != "track two" {
		t.Errorf("a listed track did not stream")
	}
	for _, name := range []string{"../secret.txt", "..\\secret.txt", "secret.txt", "Disc (Track 3).bin", ""} {
		resp := f.h.do(t, "GET", "/api/stream/"+itoa(f.disc)+"/files?name="+url.QueryEscape(name), nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("name %q: status %d, want 404", name, resp.StatusCode)
		}
	}

	// A film is not a game, and has no game files.
	film := f.h.addFile(t, "film.mkv", []byte("x"))
	resp = f.h.do(t, "GET", "/api/items/"+itoa(film)+"/files", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("a film's files: status %d, want 404", resp.StatusCode)
	}
}

func TestSavesKeepThePreviousCopy(t *testing.T) {
	f := newROMFixture(t)
	path := "/api/items/" + itoa(f.cart) + "/saves/sram"
	if resp := f.raw(t, "PUT", path, []byte("first"), ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("first save: %d %s", resp.StatusCode, body(t, resp))
	}
	resp := f.raw(t, "PUT", path, []byte("second"), "")
	var sv store.ROMSave
	decode(t, resp, &sv)
	if sv.SizeBytes != 6 || sv.Previous == nil || sv.Previous.SizeBytes != 5 {
		t.Errorf("save = %+v", sv)
	}
	if got := body(t, f.h.do(t, "GET", path, nil)); got != "second" {
		t.Errorf("current = %q", got)
	}
	if got := body(t, f.h.do(t, "GET", path+"?previous=1", nil)); got != "first" {
		t.Errorf("previous = %q", got)
	}
	var list struct {
		Saves []store.ROMSave `json:"saves"`
	}
	decode(t, f.h.do(t, "GET", "/api/items/"+itoa(f.cart)+"/saves", nil), &list)
	if len(list.Saves) != 1 || list.Saves[0].Slot != "sram" {
		t.Errorf("list = %+v", list.Saves)
	}
}

// A save state names the core that wrote it, and a slot is one of a closed
// set.
func TestSaveStateNeedsItsCore(t *testing.T) {
	f := newROMFixture(t)
	base := "/api/items/" + itoa(f.cart) + "/saves/"
	if resp := f.raw(t, "PUT", base+"state-1", []byte("s"), ""); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("state without a core: %d", resp.StatusCode)
	}
	resp := f.raw(t, "PUT", base+"state-1?core=mupen64plus_next&core_version=2.6", []byte("s"), "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("state with a core: %d %s", resp.StatusCode, body(t, resp))
	}
	resp.Body.Close()
	got := f.h.do(t, "GET", base+"state-1", nil)
	if got.Header.Get("X-LANcast-Core") != "mupen64plus_next" || got.Header.Get("X-LANcast-Core-Version") != "2.6" {
		t.Errorf("core headers = %q %q", got.Header.Get("X-LANcast-Core"), got.Header.Get("X-LANcast-Core-Version"))
	}
	got.Body.Close()
	for _, slot := range []string{"state-10", "evil", "..%2Fsram"} {
		if resp := f.raw(t, "PUT", base+slot, []byte("s"), ""); resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusNotFound {
			t.Errorf("slot %q: status %d", slot, resp.StatusCode)
		}
	}
}

// Saves are one person's: another account sees none of them and writes its
// own.
func TestSavesArePerPerson(t *testing.T) {
	f := newROMFixture(t)
	f.h.secure(t, "a good long password")
	path := "/api/items/" + itoa(f.cart) + "/saves/sram"
	if resp := f.raw(t, "PUT", path, []byte("owner's"), ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("owner save: %d %s", resp.StatusCode, body(t, resp))
	}
	m := f.h.member(t, "georgia", "another long password")
	req, _ := http.NewRequest("GET", f.h.srv.URL+path, nil)
	req.AddCookie(m.cookie)
	resp, err := f.h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("another person read the save: status %d", resp.StatusCode)
	}
}

// A game's ticket opens its files and its saves, including writing one; a
// film's ticket opens neither, and no ticket reaches another game.
func TestGameTicketReachesFilesAndSaves(t *testing.T) {
	f := newROMFixture(t)
	f.h.secure(t, "a good long password")
	tk := "Ticket " + mintTicket(t, f.h, f.disc)

	resp := f.raw(t, "GET", "/api/items/"+itoa(f.disc)+"/files", nil, tk)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("files with a ticket: %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = f.raw(t, "GET", "/api/stream/"+itoa(f.disc)+"/files?name="+url.QueryEscape("Disc (Track 1).bin"), nil, tk)
	if resp.StatusCode != http.StatusOK || body(t, resp) != "track one" {
		t.Errorf("a track with a ticket")
	}
	resp = f.raw(t, "PUT", "/api/items/"+itoa(f.disc)+"/saves/sram", []byte("memory card"), tk)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("save with a ticket: %d %s", resp.StatusCode, body(t, resp))
	}
	resp.Body.Close()
	if got := body(t, f.raw(t, "GET", "/api/items/"+itoa(f.disc)+"/saves/sram", nil, tk)); got != "memory card" {
		t.Errorf("read back with a ticket = %q", got)
	}

	// Another game, with this game's ticket.
	resp = f.raw(t, "GET", "/api/items/"+itoa(f.cart)+"/saves", nil, tk)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("another game's saves with this ticket: %d, want 401", resp.StatusCode)
	}
	// Anything else on this game.
	resp = f.raw(t, "DELETE", "/api/items/"+itoa(f.disc), nil, tk)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("deleting the game with its ticket: %d, want 401", resp.StatusCode)
	}

	// A film's ticket is not a game's ticket.
	film := f.h.addFile(t, "film.mkv", []byte("x"))
	ftk := "Ticket " + mintTicket(t, f.h, film)
	resp = f.raw(t, "GET", "/api/items/"+itoa(film)+"/saves", nil, ftk)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("a film's ticket on saves: %d, want 401", resp.StatusCode)
	}
}

func TestTicketRoutes(t *testing.T) {
	cases := []struct {
		method, path string
		id           int64
		romOnly, ok  bool
	}{
		{"GET", "/api/stream/5", 5, false, true},
		{"HEAD", "/api/stream/5", 5, false, true},
		{"GET", "/api/stream/5/files", 5, true, true},
		{"GET", "/api/items/5/files", 5, true, true},
		{"GET", "/api/items/5/saves", 5, true, true},
		{"GET", "/api/items/5/saves/sram", 5, true, true},
		{"PUT", "/api/items/5/saves/state-1", 5, true, true},
		{"POST", "/api/stream/5", 0, false, false},
		{"PUT", "/api/stream/5/files", 0, false, false},
		{"PUT", "/api/items/5/saves", 0, false, false},
		{"DELETE", "/api/items/5/saves/sram", 0, false, false},
		{"GET", "/api/items/5", 0, false, false},
		{"GET", "/api/items/5/saves/sram/x", 0, false, false},
		{"GET", "/api/stream/5/transcode", 0, false, false},
		{"GET", "/api/stream/05", 0, false, false},
		{"GET", "/api/items/5/stream-ticket", 0, false, false},
	}
	for _, c := range cases {
		id, rom, ok := ticketRoute(c.method, c.path)
		if id != c.id || rom != c.romOnly || ok != c.ok {
			t.Errorf("%s %s = (%d, %v, %v), want (%d, %v, %v)", c.method, c.path, id, rom, ok, c.id, c.romOnly, c.ok)
		}
	}
}

// Deleting an account removes its saves: the rows with the account, the
// files by the handler.
func TestDeletingAUserRemovesTheirSaves(t *testing.T) {
	f := newROMFixture(t)
	f.h.secure(t, "a good long password")
	m := f.h.member(t, "georgia", "another long password")
	req, _ := http.NewRequest("PUT", f.h.srv.URL+"/api/items/"+itoa(f.cart)+"/saves/sram", bytes.NewReader([]byte("hers")))
	req.Header.Set("Origin", f.h.srv.URL)
	req.AddCookie(m.cookie)
	resp, err := f.h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("member save: %d", resp.StatusCode)
	}
	fh, err := f.h.srvAPI.saveFiles().Open(m.id, f.cart, "sram", false)
	if err != nil {
		t.Fatalf("the save file was not written: %v", err)
	}
	// Closed before the delete: Windows will not remove a file held open.
	fh.Close()
	resp = f.h.authed(t, "DELETE", "/api/users/"+m.id, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete user: %d", resp.StatusCode)
	}
	if _, err := f.h.srvAPI.saveFiles().Open(m.id, f.cart, "sram", false); !os.IsNotExist(err) {
		t.Errorf("a deleted user's save file survived: %v", err)
	}
	if list, _ := f.h.st.ROMSaves(context.Background(), m.id, f.cart); len(list) != 0 {
		t.Errorf("a deleted user's save rows survived")
	}
}
