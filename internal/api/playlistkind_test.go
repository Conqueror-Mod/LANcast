package api

import (
	"net/http/httptest"
	"testing"
)

/*
 * The playlist says which kind it is.
 *
 * The client reads this before holding a refusal against the device: WebView2
 * plays a complete playlist and cannot play a growing one, and a copied video
 * track still gets a growing one. Without the header, three copied films would
 * settle the device as unable to play playlists, and the encoded films that now
 * play on it would never be offered one again.
 */
func TestAPlaylistSaysWhetherItIsCompleteOrGrowing(t *testing.T) {
	for _, c := range []struct {
		complete bool
		want     string
	}{{true, "complete"}, {false, "growing"}} {
		rec := httptest.NewRecorder()
		writePlaylist(rec, c.complete, "#EXTM3U\n")
		if got := rec.Header().Get(PlaylistKindHeader); got != c.want {
			t.Errorf("complete=%v: %s = %q, want %q", c.complete, PlaylistKindHeader, got, c.want)
		}
		if got := rec.Header().Get("Content-Type"); got != "application/vnd.apple.mpegurl" {
			t.Errorf("complete=%v: Content-Type = %q", c.complete, got)
		}
	}
}
