package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"lancast/internal/store"
)

/*
 * A friend's player offers what the household's does: another audio track,
 * and skipping the intro and the credits. Both need facts about the file that
 * only its own server has, so its item answer carries them.
 */
func TestFederationItemCarriesAudioTracksAndMarkers(t *testing.T) {
	f := newFedFixture(t)
	f.share(t, f.h.lib.ID, "")
	item := f.h.addFile(t, "arrival.mkv", []byte("not really a film"))
	ctx := context.Background()
	if err := f.h.st.SaveProbe(ctx, item, store.ProbeResult{
		DurationMS: 6_960_000,
		Container:  "matroska",
		VideoCodec: "h264",
		Streams: []store.MediaStream{
			{Index: 0, Kind: "video", Codec: "h264"},
			{Index: 1, Kind: "audio", Codec: "aac", Language: "eng", Channels: 6, Default: true},
			{Index: 2, Kind: "audio", Codec: "ac3", Language: "fre", Title: "Français"},
			{Index: 3, Kind: "subtitle", Codec: "subrip", Language: "eng"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	end := int64(95_000)
	if err := f.h.st.SaveMarkers(ctx, item, []string{"intro", "credits"}, []store.Marker{
		{Kind: "intro", StartMS: 5_000, EndMS: &end, Source: "test", Confidence: 1},
		{Kind: "credits", StartMS: 6_700_000, Source: "test", Confidence: 1},
	}); err != nil {
		t.Fatal(err)
	}

	r := peerRequest(t, f.georgia, "/api/federation/item/"+itoa64(item))
	r.SetPathValue("item", itoa64(item))
	w := f.call(f.h.srvAPI.federationPlay(f.h.srvAPI.federationItem), r)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var got struct {
		Audio   []store.MediaStream `json:"audio"`
		Markers []store.Marker      `json:"markers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Audio) != 2 || got.Audio[0].Index != 1 || got.Audio[1].Language != "fre" {
		t.Errorf("audio = %+v, want the two audio tracks only", got.Audio)
	}
	kinds := map[string]bool{}
	for _, m := range got.Markers {
		kinds[m.Kind] = true
	}
	if !kinds["intro"] || !kinds["credits"] {
		t.Errorf("markers = %+v, want the intro and the credits", got.Markers)
	}
}

// A file with nothing probed and nothing detected says nothing about either.
func TestFederationItemLeavesOutWhatItDoesNotKnow(t *testing.T) {
	f := newFedFixture(t)
	f.share(t, f.h.lib.ID, "")
	item := f.h.addFile(t, "unprobed.mkv", []byte("x"))

	r := peerRequest(t, f.georgia, "/api/federation/item/"+itoa64(item))
	r.SetPathValue("item", itoa64(item))
	w := f.call(f.h.srvAPI.federationPlay(f.h.srvAPI.federationItem), r)
	var got map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if _, ok := got["audio"]; ok {
		t.Error("audio present for an unprobed file")
	}
	if _, ok := got["markers"]; ok {
		t.Error("markers present for a file nothing has examined")
	}
}
