package api

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"lancast/internal/store"
)

// A channel list whose entries carry tvg-id, which is the only thing that lets
// a guide attach to them.
const taggedList = `#EXTM3U
#EXTINF:-1 tvg-id="one.example" group-title="UK",Channel One
https://provider.example/one/index.m3u8
#EXTINF:-1 tvg-id="two.example" group-title="Sports",Channel Two
https://provider.example/two/index.m3u8
#EXTINF:-1 group-title="Sports",Untagged Channel
https://provider.example/three/index.m3u8
`

// guideFor builds an XMLTV document around `now` so assertions can talk about
// "on now" without freezing the clock.
func guideFor(now time.Time) string {
	fmtT := func(t time.Time) string { return t.UTC().Format("20060102150405") + " +0000" }
	return fmt.Sprintf(`<?xml version="1.0"?>
<tv>
  <channel id="one.example"><display-name>Channel One</display-name></channel>
  <programme start="%s" stop="%s" channel="one.example">
    <title>On Now</title><desc>The current programme.</desc><category>News</category>
  </programme>
  <programme start="%s" stop="%s" channel="one.example"><title>Up Next</title></programme>
  <programme start="%s" stop="%s" channel="two.example"><title>Elsewhere</title></programme>
  <programme start="%s" stop="%s" channel="nobody.example"><title>Unsubscribed</title></programme>
</tv>`,
		fmtT(now.Add(-20*time.Minute)), fmtT(now.Add(40*time.Minute)),
		fmtT(now.Add(40*time.Minute)), fmtT(now.Add(100*time.Minute)),
		fmtT(now.Add(-10*time.Minute)), fmtT(now.Add(50*time.Minute)),
		fmtT(now.Add(-10*time.Minute)), fmtT(now.Add(50*time.Minute)))
}

type guideBody struct {
	At       int64 `json:"at"`
	Channels map[string]struct {
		Now  *store.Program `json:"now"`
		Next *store.Program `json:"next"`
	} `json:"channels"`
}

func addSourceWithGuide(t *testing.T, h *harness, list, guide string) (store.ChannelSource, int) {
	t.Helper()
	provider := upstream(t, list, "application/x-mpegurl")
	guideSrv := upstream(t, guide, "application/xml")

	var body struct {
		Source      store.ChannelSource `json:"source"`
		Channels    int                 `json:"channels"`
		Programs    int                 `json:"programs"`
		ImportError string              `json:"import_error"`
		EPGError    string              `json:"epg_error"`
	}
	resp := h.do(t, "POST", "/api/channel-sources", map[string]any{
		"name": "Provider", "url": provider.URL, "epg_url": guideSrv.URL,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	decode(t, resp, &body)
	if body.ImportError != "" || body.EPGError != "" {
		t.Fatalf("import error %q, guide error %q", body.ImportError, body.EPGError)
	}
	return body.Source, body.Programs
}

func TestAddingASourceImportsItsGuide(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	_, programs := addSourceWithGuide(t, h, taggedList, guideFor(now))

	// Three of the four listings match a channel this source carries. The
	// fourth is for a channel nobody subscribes to and is dropped rather than
	// stored against nothing.
	if programs != 3 {
		t.Fatalf("stored %d listings, want 3", programs)
	}

	var g guideBody
	decode(t, h.do(t, "GET", "/api/guide", nil), &g)
	if len(g.Channels) != 2 {
		t.Fatalf("guide covers %d channels, want 2", len(g.Channels))
	}
	for _, entry := range g.Channels {
		if entry.Now != nil && entry.Now.Title == "On Now" {
			if entry.Next == nil || entry.Next.Title != "Up Next" {
				t.Errorf("next = %+v, want Up Next", entry.Next)
			}
			if entry.Now.Description == nil || *entry.Now.Description != "The current programme." {
				t.Errorf("description = %v", entry.Now.Description)
			}
			return
		}
	}
	t.Errorf("no channel reported On Now: %+v", g.Channels)
}

// A channel with no tvg-id cannot be matched, and matching it by display name
// is exactly the guess that attaches "BBC One" listings to "BBC One HD".
func TestUntaggedChannelsGetNoListings(t *testing.T) {
	h := newHarness(t)
	addSourceWithGuide(t, h, taggedList, guideFor(time.Now()))

	var list struct {
		Channels []store.Channel `json:"channels"`
	}
	decode(t, h.do(t, "GET", "/api/channels", nil), &list)

	var untagged store.Channel
	for _, c := range list.Channels {
		if c.Name == "Untagged Channel" {
			untagged = c
		}
	}
	if untagged.ID == 0 {
		t.Fatal("the untagged channel was not imported at all")
	}
	if untagged.TvgID != nil {
		t.Errorf("tvg_id = %q, want null", *untagged.TvgID)
	}

	var g guideBody
	decode(t, h.do(t, "GET", "/api/guide", nil), &g)
	if _, present := g.Channels[strconv.FormatInt(untagged.ID, 10)]; present {
		t.Error("an untagged channel was given listings")
	}
}

// Most published XMLTV is gzipped, because the format compresses about tenfold.
// The compression is the payload rather than the transport, so Go's HTTP client
// does not unwrap it and this must.
func TestGzippedGuideIsDecompressed(t *testing.T) {
	h := newHarness(t)
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(guideFor(time.Now()))); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	// Served as text/plain, which is how providers really serve .gz files —
	// the sniff is on the bytes for exactly this reason.
	_, programs := addSourceWithGuide(t, h, taggedList, buf.String())
	if programs != 3 {
		t.Fatalf("stored %d listings from a gzipped guide, want 3", programs)
	}
}

/*
 * A broken guide must not fail the channel import.
 *
 * A working channel list with no schedule is a usable Live TV — it was the
 * whole feature for a milestone. Reporting the guide failure in the same field
 * as a playlist failure would make the channels look broken too.
 */
func TestABrokenGuideStillLeavesWorkingChannels(t *testing.T) {
	h := newHarness(t)
	provider := upstream(t, taggedList, "application/x-mpegurl")
	broken := upstream(t, "<!DOCTYPE html><html>403</html>", "text/html")

	var body struct {
		Channels    int    `json:"channels"`
		Programs    int    `json:"programs"`
		ImportError string `json:"import_error"`
		EPGError    string `json:"epg_error"`
	}
	decode(t, h.do(t, "POST", "/api/channel-sources", map[string]any{
		"name": "Provider", "url": provider.URL, "epg_url": broken.URL,
	}), &body)

	if body.Channels != 3 {
		t.Errorf("channels = %d, want 3 — a bad guide took the channel list with it", body.Channels)
	}
	if body.ImportError != "" {
		t.Errorf("import_error = %q — a guide failure was reported as a playlist failure", body.ImportError)
	}
	if body.EPGError == "" {
		t.Error("a guide that returned HTML was reported as a success")
	}
}

/*
 * Refresh imports channels first and the guide second.
 *
 * Replacing channels cascades their listings away, so the other order produces
 * an empty guide with no error to explain it. This is the assertion that
 * catches that regression.
 */
func TestRefreshKeepsTheGuide(t *testing.T) {
	h := newHarness(t)
	src, _ := addSourceWithGuide(t, h, taggedList, guideFor(time.Now()))

	var res struct {
		Channels int    `json:"channels"`
		Programs int    `json:"programs"`
		EPGError string `json:"epg_error"`
	}
	decode(t, h.do(t, "POST", fmt.Sprintf("/api/channel-sources/%d/refresh", src.ID), nil), &res)
	if res.EPGError != "" {
		t.Fatalf("guide error on refresh: %s", res.EPGError)
	}
	if res.Programs != 3 {
		t.Errorf("programs = %d after refresh, want 3", res.Programs)
	}

	var g guideBody
	decode(t, h.do(t, "GET", "/api/guide", nil), &g)
	if len(g.Channels) == 0 {
		t.Error("the guide was empty after a refresh — channels were replaced after it was imported")
	}
}

func TestChannelGuideReturnsASchedule(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	addSourceWithGuide(t, h, taggedList, guideFor(now))

	var list struct {
		Channels []store.Channel `json:"channels"`
	}
	decode(t, h.do(t, "GET", "/api/channels", nil), &list)
	id := list.Channels[0].ID

	var body struct {
		Programs []store.Program `json:"programs"`
	}
	decode(t, h.do(t, "GET", fmt.Sprintf("/api/channels/%d/guide", id), nil), &body)
	if len(body.Programs) != 2 {
		t.Fatalf("got %d programmes, want 2", len(body.Programs))
	}
	// The programme already under way is included: it is the one being watched,
	// and a schedule that omits it starts with a hole.
	if body.Programs[0].Title != "On Now" || body.Programs[1].Title != "Up Next" {
		t.Errorf("got %q, %q", body.Programs[0].Title, body.Programs[1].Title)
	}
}

func TestChannelGuideRejectsABadWindow(t *testing.T) {
	h := newHarness(t)
	addSourceWithGuide(t, h, taggedList, guideFor(time.Now()))

	var list struct {
		Channels []store.Channel `json:"channels"`
	}
	decode(t, h.do(t, "GET", "/api/channels", nil), &list)
	id := list.Channels[0].ID

	for _, q := range []string{"?hours=0", "?hours=99999", "?hours=soon", "?from=yesterday"} {
		resp := h.do(t, "GET", fmt.Sprintf("/api/channels/%d/guide%s", id, q), nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", q, resp.StatusCode)
		}
	}
}

func TestChannelGuideForAnUnknownChannelIs404(t *testing.T) {
	h := newHarness(t)
	resp := h.do(t, "GET", "/api/channels/999999/guide", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

// Setting a guide URL on an existing source imports it there and then — the
// moment somebody sets it is the moment they are watching to see if it worked.
func TestSettingAGuideURLImportsImmediately(t *testing.T) {
	h := newHarness(t)
	provider := upstream(t, taggedList, "application/x-mpegurl")

	var created struct {
		Source store.ChannelSource `json:"source"`
	}
	decode(t, h.do(t, "POST", "/api/channel-sources",
		map[string]any{"name": "Provider", "url": provider.URL}), &created)

	guideSrv := upstream(t, guideFor(time.Now()), "application/xml")
	var res struct {
		Programs int    `json:"programs"`
		EPGError string `json:"epg_error"`
	}
	resp := h.do(t, "PATCH", fmt.Sprintf("/api/channel-sources/%d", created.Source.ID),
		map[string]any{"epg_url": guideSrv.URL})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	decode(t, resp, &res)
	if res.EPGError != "" {
		t.Fatalf("guide error: %s", res.EPGError)
	}
	if res.Programs != 3 {
		t.Errorf("programs = %d, want 3", res.Programs)
	}
}

/*
 * A guide URL is fetched by this server, exactly as a playlist URL is, so it
 * gets the same refusal to point back at us. The check being on one field and
 * not the other is how a closed door grows a second entrance.
 */
func TestGuideURLCannotPointAtThisServer(t *testing.T) {
	h := newHarness(t)
	provider := upstream(t, taggedList, "application/x-mpegurl")

	own, err := url.Parse(h.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp := h.do(t, "POST", "/api/channel-sources", map[string]any{
		"name": "Provider", "url": provider.URL,
		"epg_url": "http://" + own.Host + "/api/guide",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}

	// Setting it later must be refused by the same check — a door closed on one
	// route and open on another is not closed.
	var created struct {
		Source store.ChannelSource `json:"source"`
	}
	decode(t, h.do(t, "POST", "/api/channel-sources",
		map[string]any{"name": "Later", "url": provider.URL}), &created)

	resp = h.do(t, "PATCH", fmt.Sprintf("/api/channel-sources/%d", created.Source.ID),
		map[string]any{"epg_url": "http://" + own.Host + "/api/guide"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("PATCH status = %d, want 400", resp.StatusCode)
	}
}

// A source with no guide answers an empty guide rather than an error: "nothing
// scheduled" is a state Live TV is expected to be in.
func TestGuideWithNoSourcesIsEmpty(t *testing.T) {
	h := newHarness(t)
	resp := h.do(t, "GET", "/api/guide", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var g guideBody
	decode(t, resp, &g)
	if len(g.Channels) != 0 {
		t.Errorf("got %d channels, want none", len(g.Channels))
	}
	if g.At == 0 {
		t.Error("the guide did not say what time it is describing")
	}
}
