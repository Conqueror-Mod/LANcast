package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"lancast/internal/store"
)

/*
 * A friend downloading from a share (ADR 0071, amended 2026-11-20). Watching
 * and downloading are separate grants: a share lets a friend watch, and the
 * download switch, off by default, lets them keep the file.
 */

func (f fedFixture) allowDownloads(t *testing.T, lib int64, on bool) {
	t.Helper()
	if err := f.h.st.SetShareDownloads(context.Background(), f.peerFP, lib, on); err != nil {
		t.Fatal(err)
	}
}

func TestAFriendCannotDownloadUntilTheHostAllowsIt(t *testing.T) {
	f := newFedFixture(t)
	item := f.h.addFile(t, "film.mkv", []byte("the film itself"))
	f.share(t, f.h.lib.ID, "")

	// Shared to watch, not to keep.
	w := f.call(f.h.srvAPI.federationDownload,
		peerRequest(t, f.georgia, "/api/federation/download?item="+itoa(item)))
	if w.Code != http.StatusNotFound {
		t.Fatalf("download with the switch off: status %d, want 404", w.Code)
	}
	if w.Body.Len() > 200 {
		t.Error("the refusal returned a body large enough to be the file")
	}

	f.allowDownloads(t, f.h.lib.ID, true)
	w = f.call(f.h.srvAPI.federationDownload,
		peerRequest(t, f.georgia, "/api/federation/download?item="+itoa(item)))
	if w.Code != http.StatusOK {
		t.Fatalf("download with the switch on: status %d, want 200", w.Code)
	}
	if w.Body.String() != "the film itself" {
		t.Errorf("body = %q, want the original file", w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") {
		t.Errorf("Content-Disposition = %q, want an attachment", cd)
	}

	// And off again on the next request.
	f.allowDownloads(t, f.h.lib.ID, false)
	w = f.call(f.h.srvAPI.federationDownload,
		peerRequest(t, f.georgia, "/api/federation/download?item="+itoa(item)))
	if w.Code != http.StatusNotFound {
		t.Errorf("download after switching off: status %d, want 404", w.Code)
	}
}

// A download never reaches further than a stream: the share's ceiling holds.
func TestAFriendsDownloadKeepsTheCeiling(t *testing.T) {
	f := newFedFixture(t)
	grown := f.h.addFile(t, "scream.mkv", []byte("x"))
	rr := "R"
	if err := f.h.st.UpdateItemMetadata(context.Background(), grown, store.ItemMetadata{ContentRating: &rr}); err != nil {
		t.Fatal(err)
	}
	f.share(t, f.h.lib.ID, "PG")
	f.allowDownloads(t, f.h.lib.ID, true)

	w := f.call(f.h.srvAPI.federationDownload,
		peerRequest(t, f.georgia, "/api/federation/download?item="+itoa(grown)))
	if w.Code != http.StatusNotFound {
		t.Errorf("an R film under a PG share: status %d, want 404", w.Code)
	}
}

// A server that was never paired is turned away before anything else.
func TestAStrangerCannotDownload(t *testing.T) {
	f := newFedFixture(t)
	item := f.h.addFile(t, "film.mkv", []byte("x"))
	f.share(t, f.h.lib.ID, "")
	f.allowDownloads(t, f.h.lib.ID, true)

	w := f.call(f.h.srvAPI.federationDownload,
		peerRequest(t, f.stranger, "/api/federation/download?item="+itoa(item)))
	if w.Code == http.StatusOK {
		t.Error("a stranger downloaded a file")
	}
}

// The switch can only be set on a share that exists.
func TestDownloadsNeedAShare(t *testing.T) {
	f := newFedFixture(t)
	err := f.h.st.SetShareDownloads(context.Background(), f.peerFP, f.h.lib.ID, true)
	if err != store.ErrNotShared {
		t.Errorf("allowing downloads on an unshared library: %v, want ErrNotShared", err)
	}
}

// The friend's player offers Download only where the host will answer it.
func TestAFriendsItemSaysWhetherItCanBeDownloaded(t *testing.T) {
	f := newFedFixture(t)
	item := f.h.addFile(t, "film.mkv", []byte("x"))
	f.share(t, f.h.lib.ID, "")

	downloadable := func() bool {
		t.Helper()
		r := peerRequest(t, f.georgia, "/api/federation/item/"+itoa(item))
		r.SetPathValue("item", itoa(item))
		w := f.call(f.h.srvAPI.federationPlay(f.h.srvAPI.federationItem), r)
		if w.Code != http.StatusOK {
			t.Fatalf("federation item: status %d", w.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body["downloadable"] == true
	}

	if downloadable() {
		t.Error("an item said downloadable with the switch off")
	}
	f.allowDownloads(t, f.h.lib.ID, true)
	if !downloadable() {
		t.Error("an item did not say downloadable with the switch on")
	}
}

// Re-saving a share's ceiling leaves its download switch where it was, and
// the switch is listed on the host's sharing screen.
func TestSavingACeilingKeepsTheDownloadSwitch(t *testing.T) {
	f := newShareFixture(t)
	path := "/api/peers/" + f.peerFP + "/shares/" + itoa(f.videoID)

	for _, body := range []map[string]any{
		{"ceiling": "", "downloads": true},
		{"ceiling": "PG-13"},
	} {
		resp := f.h.authed(t, "PUT", path, body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("PUT %v: status %d", body, resp.StatusCode)
		}
	}

	var got struct {
		Libraries []struct {
			ID        int64  `json:"id"`
			Ceiling   string `json:"ceiling"`
			Downloads bool   `json:"downloads"`
		} `json:"libraries"`
	}
	decode(t, f.h.authed(t, "GET", "/api/peers/"+f.peerFP+"/shares", nil), &got)
	for _, l := range got.Libraries {
		if l.ID == f.videoID && (!l.Downloads || l.Ceiling != "PG-13") {
			t.Errorf("after re-saving the ceiling: downloads=%v ceiling=%q, want true/PG-13", l.Downloads, l.Ceiling)
		}
	}
}

// A far server's filename is re-sanitised before it becomes our header.
func TestARelayedFilenameIsSanitised(t *testing.T) {
	for _, c := range []struct{ header, want string }{
		{`attachment; filename="Film (2001).mkv"`, "Film (2001).mkv"},
		{`attachment; filename="..\..\evil.exe"`, "-..-evil.exe"},
		{`attachment; filename="a/b:c.mkv"`, "a-b-c.mkv"},
		{`attachment; filename*=UTF-8''Am%C3%A9lie.mkv`, "Amélie.mkv"},
		{`garbage`, "download"},
		{``, "download"},
	} {
		if got := relayedFilename(c.header); got != c.want {
			t.Errorf("relayedFilename(%q) = %q, want %q", c.header, got, c.want)
		}
	}
}
