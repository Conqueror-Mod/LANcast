package livetv

import (
	"errors"
	"strings"
	"testing"
)

// The ordinary provider file, with the attributes that make a channel list
// navigable rather than a wall of names.
func TestParseIPTVList(t *testing.T) {
	const file = `#EXTM3U
#EXTINF:-1 tvg-id="bbc1.uk" tvg-logo="https://logos.example/bbc1.png" group-title="UK",BBC One
https://stream.example/bbc1/index.m3u8
#EXTINF:-1 tvg-id="sky.sports" tvg-logo="https://logos.example/sky.png" group-title="Sports",Sky Sports Main Event
https://stream.example/sky/index.m3u8
`
	got, err := Parse(strings.NewReader(file))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("channels = %d, want 2", len(got))
	}
	if got[0].Name != "BBC One" {
		t.Errorf("name = %q, want BBC One", got[0].Name)
	}
	if got[0].Group != "UK" {
		t.Errorf("group = %q, want UK", got[0].Group)
	}
	if got[0].LogoURL != "https://logos.example/bbc1.png" {
		t.Errorf("logo = %q", got[0].LogoURL)
	}
	if got[1].URL != "https://stream.example/sky/index.m3u8" {
		t.Errorf("url = %q", got[1].URL)
	}
}

// Channel names contain commas — "BBC One HD, London" is a real listing — so
// the title is everything after the *first* comma. Splitting on the last one
// truncates the name and nobody notices until they look for it.
func TestChannelNameKeepsItsCommas(t *testing.T) {
	const file = `#EXTM3U
#EXTINF:-1 group-title="UK",BBC One HD, London
https://stream.example/one
`
	got, err := Parse(strings.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Name != "BBC One HD, London" {
		t.Errorf("name = %q, want the whole name including its comma", got[0].Name)
	}
}

/*
 * A logo URL with a comma and an equals sign in it.
 *
 * This is why the attribute reader is written by hand rather than as a regular
 * expression: a greedy match here swallows the group name, and the symptom is a
 * channel list where every group is empty and every logo is broken.
 */
func TestAttributesSurviveAwkwardValues(t *testing.T) {
	const file = `#EXTM3U
#EXTINF:-1 tvg-logo="https://cdn.example/img?id=1,2&size=large" group-title="News",Channel 4
https://stream.example/four
`
	got, err := Parse(strings.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	if got[0].LogoURL != "https://cdn.example/img?id=1,2&size=large" {
		t.Errorf("logo = %q, want the whole URL", got[0].LogoURL)
	}
	if got[0].Group != "News" {
		t.Errorf("group = %q, want News — the logo swallowed it", got[0].Group)
	}
}

// One malformed line is not a reason to refuse the other entries: these files
// are written by dozens of tools and always contain something unexpected.
func TestOneBadLineDoesNotLoseTheRest(t *testing.T) {
	const file = `#EXTM3U
#EXTINF:this is not a duration
#EXTVLCOPT:network-caching=1000
#EXTINF:-1,Working Channel
https://stream.example/works
`
	got, err := Parse(strings.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "Working Channel" {
		t.Errorf("channels = %+v, want the one good entry", got)
	}
}

// A URL with no #EXTINF is still a channel; naming it after its host beats an
// empty row.
func TestBareURLGetsAName(t *testing.T) {
	got, err := Parse(strings.NewReader("#EXTM3U\nhttps://stream.example/anon/index.m3u8\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "stream.example" {
		t.Errorf("channels = %+v, want one named after its host", got)
	}
}

/*
 * The failure that actually happens: a lapsed subscription serves an HTML error
 * page, and the importer is handed it with a 200.
 *
 * Importing that as one channel called "<!DOCTYPE html>" is worse than
 * refusing, because it looks like it worked.
 */
func TestHTMLIsRefused(t *testing.T) {
	const page = `<!DOCTYPE html>
<html><body><h1>403 Forbidden</h1></body></html>`
	_, err := Parse(strings.NewReader(page))
	if !errors.Is(err, ErrNotAPlaylist) {
		t.Errorf("err = %v, want ErrNotAPlaylist", err)
	}
}

// A media playlist of local files handed to the wrong importer produces no
// channels rather than channels that can never play.
func TestLocalPathsAreNotChannels(t *testing.T) {
	const file = `#EXTM3U
#EXTINF:214,Some Song
D:\Music\song.mp3
`
	got, err := Parse(strings.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("channels = %+v, want none from a file of local paths", got)
	}
}

// A header-only file is a valid empty list, not an error: a provider between
// subscriptions serves exactly this.
func TestEmptyListIsNotAnError(t *testing.T) {
	got, err := Parse(strings.NewReader("#EXTM3U\n"))
	if err != nil {
		t.Fatalf("err = %v, want nil for an empty but well-formed list", err)
	}
	if len(got) != 0 {
		t.Errorf("channels = %+v, want none", got)
	}
}

// Ten thousand channels is a real provider list, and importing it whole makes
// every page that lists channels unusable.
func TestListIsBounded(t *testing.T) {
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	for i := 0; i < maxChannels+50; i++ {
		b.WriteString("#EXTINF:-1,Channel\nhttps://stream.example/c\n")
	}
	got, err := Parse(strings.NewReader(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != maxChannels {
		t.Errorf("channels = %d, want the cap of %d", len(got), maxChannels)
	}
}
