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

// No playlists among a friend's top level: a playlist is a person's own list,
// and nothing on a friend's screen opens one.
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

/*
 * A friend sees the host's collections and can open one.
 *
 * Reported from a real pairing: the host's franchises were missing from the
 * friend's view of a shared film library. They were left out on purpose,
 * because nothing could open one; `parent` now does, through the membership
 * table, under the same scope and ceiling as everything else.
 */
func seedSharedCollection(t *testing.T, h *harness) (coll int64, members []int64) {
	t.Helper()
	ctx := context.Background()
	coll, _, err := h.st.EnsureCollection(ctx, h.lib.ID, "tmdb", "c1", "Saga Collection", "saga collection")
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"saga-1.mkv", "saga-2.mkv"} {
		id := h.addFile(t, name, []byte("f"))
		year := 2001 + i
		if err := h.st.UpdateItemMetadata(ctx, id, store.ItemMetadata{Year: &year}); err != nil {
			t.Fatal(err)
		}
		if err := h.st.AddToCollection(ctx, id, coll, 0); err != nil {
			t.Fatal(err)
		}
		members = append(members, id)
	}
	return coll, members
}

func guestIDs(t *testing.T, f redeemFixture, token, query string) []int64 {
	t.Helper()
	var got struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	decode(t, f.asGuest(t, token, http.MethodGet, "/api/guest/items?"+query), &got)
	out := make([]int64, 0, len(got.Items))
	for _, it := range got.Items {
		out = append(out, it.ID)
	}
	return out
}

func TestAGuestSeesAndOpensASharedCollection(t *testing.T) {
	f := newRedeemFixture(t)
	token := guestToken(t, f)
	peer := identity.Normalize(f.georgia.Fingerprint())
	coll, members := seedSharedCollection(t, f.h)
	if err := f.h.st.ShareLibrary(context.Background(), peer, f.h.lib.ID, "", time.Now()); err != nil {
		t.Fatal(err)
	}

	has := func(ids []int64, want int64) bool {
		for _, id := range ids {
			if id == want {
				return true
			}
		}
		return false
	}
	lib := "library=" + itoa(f.h.lib.ID)

	// Not in the browse grid, so the total counts films, as the household's
	// own grid does: a friend's read 1,440 over a library of 1,253.
	var grid struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
		Total int `json:"total"`
	}
	decode(t, f.asGuest(t, token, http.MethodGet, "/api/guest/items?"+lib), &grid)
	if grid.Total != len(members) {
		t.Errorf("grid total = %d, want %d films and no collection", grid.Total, len(members))
	}
	for _, it := range grid.Items {
		if it.ID == coll {
			t.Error("a collection is in a friend's browse grid")
		}
	}
	// Listed on their own, which is the friend's Collections view.
	if got := guestIDs(t, f, token, lib+"&kind=collection"); len(got) != 1 || got[0] != coll {
		t.Errorf("kind=collection = %v, want only %d", got, coll)
	}
	// And found by name.
	if !has(guestIDs(t, f, token, lib+"&q=Saga"), coll) {
		t.Error("a search for the collection's name does not find it")
	}

	inside := guestIDs(t, f, token, "library="+itoa(f.h.lib.ID)+"&parent="+itoa(coll))
	if len(inside) != 2 || inside[0] != members[0] || inside[1] != members[1] {
		t.Errorf("inside the collection = %v, want %v in release order", inside, members)
	}
}

// A member above the share's ceiling stays out, reached through a collection
// or not.
func TestAGuestsCollectionKeepsTheCeiling(t *testing.T) {
	f := newRedeemFixture(t)
	token := guestToken(t, f)
	ctx := context.Background()
	peer := identity.Normalize(f.georgia.Fingerprint())
	coll, members := seedSharedCollection(t, f.h)
	for i, r := range []string{"PG", "R"} {
		r := r
		if err := f.h.st.UpdateItemMetadata(ctx, members[i], store.ItemMetadata{ContentRating: &r}); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.h.st.ShareLibrary(ctx, peer, f.h.lib.ID, "PG", time.Now()); err != nil {
		t.Fatal(err)
	}
	inside := guestIDs(t, f, token, "library="+itoa(f.h.lib.ID)+"&parent="+itoa(coll))
	if len(inside) != 1 || inside[0] != members[0] {
		t.Errorf("inside a collection under a PG share = %v, want only %d", inside, members[0])
	}
}

// A collection in a library that was not shared is a 404, like any container.
func TestAGuestCannotOpenACollectionInAnUnsharedLibrary(t *testing.T) {
	f := newRedeemFixture(t)
	token := guestToken(t, f)
	ctx := context.Background()
	peer := identity.Normalize(f.georgia.Fingerprint())
	coll, _ := seedSharedCollection(t, f.h)
	shared, err := f.h.st.CreateLibrary(ctx, "Shared", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.h.st.ShareLibrary(ctx, peer, shared.ID, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	resp := f.asGuest(t, token, http.MethodGet,
		"/api/guest/items?library="+itoa(shared.ID)+"&parent="+itoa(coll))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("a collection from an unshared library: status %d, want 404", resp.StatusCode)
	}
}

// Release order, not the alphabet: "28 Weeks Later" (2007) after "28 Days
// Later" (2002) happens to agree with both, so the films here disagree.
func TestAGuestsCollectionIsInReleaseOrder(t *testing.T) {
	f := newRedeemFixture(t)
	token := guestToken(t, f)
	ctx := context.Background()
	peer := identity.Normalize(f.georgia.Fingerprint())
	coll, _, err := f.h.st.EnsureCollection(ctx, f.h.lib.ID, "tmdb", "c2", "Order Collection", "order collection")
	if err != nil {
		t.Fatal(err)
	}
	var want []int64
	for _, c := range []struct {
		name string
		year int
	}{{"zebra.mkv", 1999}, {"apple.mkv", 2004}, {"mango.mkv", 2010}} {
		id := f.h.addFile(t, c.name, []byte("f"))
		y := c.year
		if err := f.h.st.UpdateItemMetadata(ctx, id, store.ItemMetadata{Year: &y}); err != nil {
			t.Fatal(err)
		}
		if err := f.h.st.AddToCollection(ctx, id, coll, 0); err != nil {
			t.Fatal(err)
		}
		want = append(want, id)
	}
	if err := f.h.st.ShareLibrary(ctx, peer, f.h.lib.ID, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	got := guestIDs(t, f, token, "library="+itoa(f.h.lib.ID)+"&parent="+itoa(coll))
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("collection order = %v, want release order %v", got, want)
	}
}
