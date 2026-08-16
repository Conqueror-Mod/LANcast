package livetv

import (
	"strings"
	"testing"
	"time"
)

const sampleGuide = `<?xml version="1.0" encoding="UTF-8"?>
<tv generator-info-name="test">
  <channel id="bbcone.uk"><display-name>BBC One</display-name></channel>
  <programme start="20260815190000 +0100" stop="20260815200000 +0100" channel="bbcone.uk">
    <title>The News</title>
    <desc>What happened.</desc>
    <category>News</category>
    <icon src="https://example.invalid/news.png" />
  </programme>
  <programme start="20260815200000 +0100" stop="20260815210000 +0100" channel="bbcone.uk">
    <title>Some Drama</title>
    <episode-num system="xmltv_ns">2.4.0/1</episode-num>
  </programme>
</tv>`

func TestParseXMLTVReadsAProgramme(t *testing.T) {
	progs, err := ParseXMLTV(strings.NewReader(sampleGuide))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(progs) != 2 {
		t.Fatalf("got %d programmes, want 2", len(progs))
	}

	p := progs[0]
	if p.ChannelID != "bbcone.uk" || p.Title != "The News" {
		t.Errorf("got %+v", p)
	}
	if p.Desc != "What happened." || p.Category != "News" {
		t.Errorf("desc/category: %+v", p)
	}
	if p.IconURL != "https://example.invalid/news.png" {
		t.Errorf("icon: %q", p.IconURL)
	}
	want := time.Date(2026, 8, 15, 19, 0, 0, 0, time.FixedZone("", 3600))
	if !p.Start.Equal(want) {
		t.Errorf("start = %v, want %v", p.Start, want)
	}
	if p.Stop.Sub(p.Start) != time.Hour {
		t.Errorf("duration = %v", p.Stop.Sub(p.Start))
	}
}

// xmltv_ns counts from zero and everything else counts from one. Getting this
// backwards labels every episode one short, which reads as plausible.
func TestEpisodeNumberingIsOneBased(t *testing.T) {
	progs, err := ParseXMLTV(strings.NewReader(sampleGuide))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := progs[1]; got.Season != 3 || got.Episode != 5 {
		t.Errorf("2.4.0/1 became S%02dE%02d, want S03E05", got.Season, got.Episode)
	}
}

// `onscreen` numbering is free text and deliberately not parsed. The programme
// still imports; it simply carries no numbers.
func TestOnscreenEpisodeNumbersAreIgnored(t *testing.T) {
	doc := `<tv><programme start="20260815190000 +0000" stop="20260815200000 +0000" channel="c">
	  <title>T</title><episode-num system="onscreen">S03E04</episode-num></programme></tv>`
	progs, err := ParseXMLTV(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if progs[0].Season != 0 || progs[0].Episode != 0 {
		t.Errorf("onscreen numbering was parsed: %+v", progs[0])
	}
}

// An HTML error page from a lapsed subscription must not import as an empty
// guide — that looks like a schedule with nothing on it.
func TestParseXMLTVRejectsHTML(t *testing.T) {
	_, err := ParseXMLTV(strings.NewReader("<!DOCTYPE html><html><body>403 Forbidden</body></html>"))
	if err == nil {
		t.Fatal("an HTML page parsed as a guide")
	}
}

// A guide with a <tv> root and no programmes is a real thing — a grabber that
// found nothing — and is not an error.
func TestEmptyGuideIsNotAnError(t *testing.T) {
	progs, err := ParseXMLTV(strings.NewReader(`<tv></tv>`))
	if err != nil {
		t.Fatalf("empty guide: %v", err)
	}
	if len(progs) != 0 {
		t.Fatalf("got %d programmes", len(progs))
	}
}

// A programme with no stop time gets an hour rather than being dropped: a hole
// in the guide reads as "nothing is on", which is a worse lie than an hour.
func TestMissingStopGetsAnHour(t *testing.T) {
	doc := `<tv><programme start="20260815190000 +0000" channel="c"><title>T</title></programme></tv>`
	progs, err := ParseXMLTV(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(progs) != 1 {
		t.Fatalf("got %d programmes", len(progs))
	}
	if d := progs[0].Stop.Sub(progs[0].Start); d != time.Hour {
		t.Errorf("duration = %v, want 1h", d)
	}
}

// Entries that cannot be placed are skipped, and skipping one must not discard
// the ones around it.
func TestUnplaceableProgrammesAreSkipped(t *testing.T) {
	doc := `<tv>
	  <programme stop="20260815200000 +0000" channel="c"><title>No start</title></programme>
	  <programme start="20260815190000 +0000" channel=""><title>No channel</title></programme>
	  <programme start="20260815210000 +0000" stop="20260815220000 +0000" channel="c"><title>Fine</title></programme>
	</tv>`
	progs, err := ParseXMLTV(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(progs) != 1 || progs[0].Title != "Fine" {
		t.Fatalf("got %+v", progs)
	}
}

// Guides carry HTML entities in synopses. Refusing a fortnight of listings over
// one &eacute; is not a trade worth making.
func TestHTMLEntitiesDoNotFailTheParse(t *testing.T) {
	doc := `<tv><programme start="20260815190000 +0000" stop="20260815200000 +0000" channel="c">
	  <title>Caf&eacute; Society</title><desc>Nice&nbsp;film.</desc></programme></tv>`
	progs, err := ParseXMLTV(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !strings.HasPrefix(progs[0].Title, "Caf") {
		t.Errorf("title = %q", progs[0].Title)
	}
}

func TestParseXMLTVTime(t *testing.T) {
	cases := []struct {
		in   string
		want time.Time
		ok   bool
	}{
		{"20260815143000 +0100", time.Date(2026, 8, 15, 14, 30, 0, 0, time.FixedZone("", 3600)), true},
		{"20260815143000 -0500", time.Date(2026, 8, 15, 14, 30, 0, 0, time.FixedZone("", -5*3600)), true},
		{"20260815143000 +01:00", time.Date(2026, 8, 15, 14, 30, 0, 0, time.FixedZone("", 3600)), true},
		{"20260815143000 Z", time.Date(2026, 8, 15, 14, 30, 0, 0, time.UTC), true},
		// Truncated forms are legal XMLTV; the missing tail is zero.
		{"20260815", time.Date(2026, 8, 15, 0, 0, 0, 0, time.Local), true},
		{"202608151430", time.Date(2026, 8, 15, 14, 30, 0, 0, time.Local), true},
		// A named zone is ambiguous across countries and is refused rather than
		// placed in the wrong hemisphere.
		{"20260815143000 BST", time.Time{}, false},
		{"", time.Time{}, false},
		{"not a time", time.Time{}, false},
	}
	for _, c := range cases {
		got, ok := ParseXMLTVTime(c.in)
		if ok != c.ok {
			t.Errorf("%q: ok = %v, want %v", c.in, ok, c.ok)
			continue
		}
		if ok && !got.Equal(c.want) {
			t.Errorf("%q = %v, want %v", c.in, got, c.want)
		}
	}
}

// No offset means this server's local time — the reading that is right for the
// case that produces it, a tuner on this network listing local television.
func TestTimeWithoutOffsetIsLocal(t *testing.T) {
	got, ok := ParseXMLTVTime("20260815143000")
	if !ok {
		t.Fatal("rejected a timestamp with no offset")
	}
	if got.Location() != time.Local {
		t.Errorf("location = %v, want Local", got.Location())
	}
}

// tvg-id is the join to the guide, and the M3U parser has to keep it.
func TestParseKeepsTvgID(t *testing.T) {
	m3u := "#EXTM3U\n#EXTINF:-1 tvg-id=\"bbcone.uk\" tvg-logo=\"http://x/l.png\" group-title=\"UK\",BBC One\nhttp://example.invalid/1\n"
	chans, err := Parse(strings.NewReader(m3u))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(chans) != 1 || chans[0].TvgID != "bbcone.uk" {
		t.Fatalf("got %+v", chans)
	}
	// The neighbouring attributes must not have been eaten by the new one.
	if chans[0].LogoURL != "http://x/l.png" || chans[0].Group != "UK" || chans[0].Name != "BBC One" {
		t.Errorf("other attributes changed: %+v", chans[0])
	}
}
