package api

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"lancast/internal/identity"
	"lancast/internal/store"
)

/*
 * A friend opens a show and sees its episodes.
 *
 * Their TV and music libraries were walls of shows and artists that could not
 * be opened: a tile could only play, and a show has nothing to play. `parent`
 * lists what is inside, under the same scope and ceiling as everything else.
 */
func seedSharedShow(t *testing.T, h *harness, rating string) (show int64, eps []int64) {
	t.Helper()
	ctx := context.Background()
	show, _, err := h.st.EnsureShow(ctx, h.lib.ID, filepath.Join(h.dir, "Show "+rating), "Show "+rating, "show "+rating)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.st.UpdateItemMetadata(ctx, show, store.ItemMetadata{ContentRating: &rating}); err != nil {
		t.Fatal(err)
	}
	series := "Show " + rating
	for i := 1; i <= 2; i++ {
		s, e := 1, i
		id, err := h.st.UpsertItem(ctx, store.ScanFile{
			LibraryID: h.lib.ID, Path: filepath.Join(h.dir, "Show "+rating, "e"+itoa(int64(i))+".mkv"),
			Kind: "episode", Title: "Episode " + itoa(int64(i)), SortTitle: series,
			Series: &series, Season: &s, Episode: &e, Container: "mkv", SizeBytes: 1, MTime: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.st.SetParent(ctx, id, &show); err != nil {
			t.Fatal(err)
		}
		eps = append(eps, id)
	}
	return show, eps
}

func TestAGuestOpensASharedShow(t *testing.T) {
	f := newRedeemFixture(t)
	token := guestToken(t, f)
	peer := identity.Normalize(f.georgia.Fingerprint())
	show, eps := seedSharedShow(t, f.h, "G")
	if err := f.h.st.ShareLibrary(context.Background(), peer, f.h.lib.ID, "PG", time.Now()); err != nil {
		t.Fatal(err)
	}

	var got struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	decode(t, f.asGuest(t, token, http.MethodGet,
		"/api/guest/items?library="+itoa(f.h.lib.ID)+"&parent="+itoa(show)), &got)
	seen := map[int64]bool{}
	for _, it := range got.Items {
		seen[it.ID] = true
	}
	if len(got.Items) != 2 || !seen[eps[0]] || !seen[eps[1]] {
		t.Errorf("a shared show's episodes = %v, want both %v", got.Items, eps)
	}
}

// A show above the share's ceiling is not there to open -- not even to learn
// the names of its seasons.
func TestAGuestCannotOpenAShowAboveTheCeiling(t *testing.T) {
	f := newRedeemFixture(t)
	token := guestToken(t, f)
	peer := identity.Normalize(f.georgia.Fingerprint())
	show, _ := seedSharedShow(t, f.h, "R")
	if err := f.h.st.ShareLibrary(context.Background(), peer, f.h.lib.ID, "PG", time.Now()); err != nil {
		t.Fatal(err)
	}
	resp := f.asGuest(t, token, http.MethodGet,
		"/api/guest/items?library="+itoa(f.h.lib.ID)+"&parent="+itoa(show))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("an R show under a PG share: status %d, want 404", resp.StatusCode)
	}
}

// A container in a library that was never shared is a 404 too.
func TestAGuestCannotOpenAShowInAnUnsharedLibrary(t *testing.T) {
	f := newRedeemFixture(t)
	token := guestToken(t, f)
	ctx := context.Background()
	peer := identity.Normalize(f.georgia.Fingerprint())
	show, _ := seedSharedShow(t, f.h, "G")
	shared, err := f.h.st.CreateLibrary(ctx, "Shared", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.h.st.ShareLibrary(ctx, peer, shared.ID, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	resp := f.asGuest(t, token, http.MethodGet,
		"/api/guest/items?library="+itoa(shared.ID)+"&parent="+itoa(show))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("a show from an unshared library: status %d, want 404", resp.StatusCode)
	}
}

// No playlists or collections among a friend's top level: they group through
// tables of their own, so there is nothing inside one to open, and every
// member is listed anyway.
func TestAGuestsTopLevelLeavesOutPlaylists(t *testing.T) {
	f := newRedeemFixture(t)
	token := guestToken(t, f)
	ctx := context.Background()
	peer := identity.Normalize(f.georgia.Fingerprint())
	film := f.h.addFile(t, "film.mkv", []byte("f"))
	list, err := f.h.st.EnsurePlaylist(ctx, f.h.lib.ID, filepath.Join(f.h.dir, "mix.m3u"), "Mix", "mix")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.h.st.ShareLibrary(ctx, peer, f.h.lib.ID, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	decode(t, f.asGuest(t, token, http.MethodGet, "/api/guest/items?library="+itoa(f.h.lib.ID)), &got)
	seen := map[int64]bool{}
	for _, it := range got.Items {
		seen[it.ID] = true
	}
	if !seen[film] {
		t.Error("the film is missing from a friend's top level")
	}
	if seen[list] {
		t.Error("a playlist was offered to a friend, with nothing inside it they could open")
	}
}
