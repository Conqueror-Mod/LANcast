package dlna

import (
	"strings"
	"testing"
)

var dev = Device{UUID: "11111111-2222-5333-8444-555555555555", Location: "http://192.168.1.20:8200/dlna/device.xml", Server: "Windows/10 UPnP/1.0 LANcast/1"}

// The shape VLC and most TVs send. MAN is quoted, which is the spec's spelling
// and the one a naive comparison gets wrong.
const vlcSearch = "M-SEARCH * HTTP/1.1\r\n" +
	"HOST: 239.255.255.250:1900\r\n" +
	"MAN: \"ssdp:discover\"\r\n" +
	"MX: 1\r\n" +
	"ST: urn:schemas-upnp-org:device:MediaServer:1\r\n\r\n"

func TestASearchForAMediaServerIsAnswered(t *testing.T) {
	s, ok := ParseSearch([]byte(vlcSearch))
	if !ok || s.Target != DeviceType {
		t.Fatalf("ParseSearch = %+v, %v", s, ok)
	}
	got := dev.Answers(s)
	if len(got) != 1 || got[0] != DeviceType {
		t.Fatalf("Answers = %v", got)
	}
	resp := string(dev.SearchResponse(got[0]))
	for _, want := range []string{
		"HTTP/1.1 200 OK\r\n",
		"LOCATION: " + dev.Location + "\r\n",
		"ST: " + DeviceType + "\r\n",
		"USN: uuid:" + dev.UUID + "::" + DeviceType + "\r\n",
	} {
		if !strings.Contains(resp, want) {
			t.Errorf("response lacks %q:\n%s", want, resp)
		}
	}
	if !strings.HasSuffix(resp, "\r\n\r\n") {
		t.Error("response is not terminated by an empty line")
	}
}

func TestSsdpAllIsAnsweredWithEveryTargetAndOthersWithNothing(t *testing.T) {
	if got := dev.Answers(Search{Target: "ssdp:all"}); len(got) != 5 {
		t.Errorf("ssdp:all answered with %d targets, want 5", len(got))
	}
	if got := dev.Answers(Search{Target: "urn:schemas-upnp-org:device:Printer:1"}); got != nil {
		t.Errorf("a printer search was answered: %v", got)
	}
	// The uuid target's USN is the uuid alone, not uuid::uuid.
	if r := string(dev.SearchResponse("uuid:" + dev.UUID)); !strings.Contains(r, "USN: uuid:"+dev.UUID+"\r\n") {
		t.Errorf("uuid target USN wrong:\n%s", r)
	}
}

func TestNotSearchesAreIgnored(t *testing.T) {
	for _, p := range []string{
		"NOTIFY * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nNT: upnp:rootdevice\r\n\r\n",
		"M-SEARCH * HTTP/1.1\r\nST: ssdp:all\r\n\r\n", // no MAN
		"garbage",
	} {
		if _, ok := ParseSearch([]byte(p)); ok {
			t.Errorf("treated as a search: %q", p)
		}
	}
}

func TestByebyeCarriesNoLocation(t *testing.T) {
	alive := string(dev.Notify(rootDevice, true))
	bye := string(dev.Notify(rootDevice, false))
	if !strings.Contains(alive, "NTS: ssdp:alive") || !strings.Contains(alive, "LOCATION: ") {
		t.Errorf("alive:\n%s", alive)
	}
	if !strings.Contains(bye, "NTS: ssdp:byebye") || strings.Contains(bye, "LOCATION") {
		t.Errorf("byebye:\n%s", bye)
	}
	if n := len(dev.Notifies(true)); n != 5 {
		t.Errorf("burst of %d, want 5", n)
	}
}

func TestUUIDIsStableAndNameBased(t *testing.T) {
	var b [16]byte
	for i := range b {
		b[i] = byte(i * 17)
	}
	u := UUIDFrom(b)
	if u != UUIDFrom(b) {
		t.Fatal("not stable")
	}
	parts := strings.Split(u, "-")
	if len(parts) != 5 || parts[2][0] != '5' || !strings.ContainsAny(parts[3][:1], "89ab") {
		t.Errorf("uuid %q is not a well-formed v5", u)
	}
}

func TestDIDLEscapesTitlesAndTheEnvelopeEscapesDIDL(t *testing.T) {
	didl := DIDL([]Object{
		{ID: "item-7", ParentID: "lib-1", Title: "Tom & Jerry <Uncut>", Class: "object.item.videoItem.movie",
			Res: &Resource{URL: "http://h:8200/dlna/media/7", MIME: "video/x-matroska", Size: 99, Duration: Duration(5025500)}},
		{ID: "lib-1", ParentID: "0", Title: "Films", Class: "object.container.storageFolder", Container: true},
	})
	if !strings.Contains(didl, "Tom &amp; Jerry &lt;Uncut&gt;") {
		t.Errorf("title not escaped: %s", didl)
	}
	if !strings.Contains(didl, `duration="1:23:45.500"`) || !strings.Contains(didl, `size="99"`) {
		t.Errorf("resource attributes: %s", didl)
	}
	if !strings.Contains(didl, `<container id="lib-1" parentID="0"`) {
		t.Errorf("container: %s", didl)
	}
	env := Response(ContentDirectory, "Browse", [][2]string{{"Result", didl}, {"NumberReturned", "2"}})
	// The DIDL travels as text inside Result, so its markup is escaped once more.
	if !strings.Contains(env, "<Result>&lt;DIDL-Lite") || strings.Contains(env, "<Result><DIDL-Lite") {
		t.Errorf("Result not escaped as text:\n%s", env[:300])
	}
}

// A Browse body as a TV sends it.
const browseBody = `<?xml version="1.0"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">
<s:Body><u:Browse xmlns:u="urn:schemas-upnp-org:service:ContentDirectory:1">
<ObjectID>lib-3</ObjectID><BrowseFlag>BrowseDirectChildren</BrowseFlag><Filter>*</Filter>
<StartingIndex>20</StartingIndex><RequestedCount>10</RequestedCount><SortCriteria></SortCriteria>
</u:Browse></s:Body></s:Envelope>`

func TestABrowseIsParsed(t *testing.T) {
	action, args, err := ParseAction(strings.NewReader(browseBody))
	if err != nil || action != "Browse" {
		t.Fatalf("ParseAction = %q, %v", action, err)
	}
	b := ParseBrowse(args)
	if b.ObjectID != "lib-3" || b.Metadata || b.Start != 20 || b.Count != 10 {
		t.Errorf("ParseBrowse = %+v", b)
	}
	if got := ParseBrowse(map[string]string{}); got.ObjectID != "0" || got.Count != 0 {
		t.Errorf("empty Browse = %+v, want the root, all", got)
	}
}

func TestMIMEFollowsTheContainer(t *testing.T) {
	cases := map[string]string{"mkv": "video/x-matroska", "MP4": "video/mp4", "flac": "audio/flac", "jpg": "image/jpeg", "xyz": "application/octet-stream"}
	for c, want := range cases {
		if got := MIME("", c); got != want {
			t.Errorf("MIME(%q) = %q, want %q", c, got, want)
		}
	}
}
