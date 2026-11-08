package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"lancast/internal/identity"
)

/*
 * A friend's poster crosses both servers: their client asks their server,
 * which asks ours.
 *
 * Reported twice as a shared library of lettered tiles. #859 made the listing
 * carry poster hashes, and its test stopped there. The image route then
 * checked the hash against an item loaded by GetItem, which never loads
 * artwork, so every poster was refused. The unit test of that check built its
 * item with artwork already filled in, which is how it passed. Only a fetch
 * through both servers can see this, so that is what this does.
 */
func TestAFriendsPosterCrossesTwoServers(t *testing.T) {
	x := newTwoServers(t)
	ctx := context.Background()

	shared, _, _, _, err := x.host.art.Put(solidJPEG(t, displayBlue))
	if err != nil {
		t.Fatal(err)
	}
	if err := x.host.st.PutArtwork(ctx, x.item, shared, "poster", "", 1, 1, 1); err != nil {
		t.Fatal(err)
	}
	// Another film's poster, in the same library: real, stored, and not this item's.
	other := x.host.addFile(t, "another film.mkv", []byte("y"))
	elsewhere, _, _, _, err := x.host.art.Put(solidJPEG(t, fileRed))
	if err != nil {
		t.Fatal(err)
	}
	if err := x.host.st.PutArtwork(ctx, other, elsewhere, "poster", "", 1, 1, 1); err != nil {
		t.Fatal(err)
	}

	guestFP := identity.Normalize(x.guest.srvAPI.ident.Fingerprint())
	if err := x.host.st.ShareLibrary(ctx, guestFP, x.host.lib.ID, "", time.Now()); err != nil {
		t.Fatal(err)
	}

	for _, size := range []string{"poster", "thumb", "poster2x"} {
		code, body := x.guestCall(t, "GET", "/artwork/"+itoa(x.item)+"/"+shared+"?size="+size, nil)
		if code != http.StatusOK || len(body) == 0 {
			t.Errorf("size=%s: status %d, %d bytes; want the poster", size, code, len(body))
		}
	}

	// The item is still not a key to every image: naming this film with the
	// other film's poster is refused, exactly as before.
	if code, _ := x.guestCall(t, "GET", "/artwork/"+itoa(x.item)+"/"+elsewhere, nil); code != http.StatusNotFound {
		t.Errorf("another item's poster through this item: status %d, want 404", code)
	}
}
