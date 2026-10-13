/*
 * Package dlna is the protocol half of DLNA (ADR 0086): SSDP discovery
 * messages, the UPnP description documents, and DIDL-Lite listings.
 *
 * Everything here is string-building and parsing, with no store, no HTTP
 * server and no sockets except in the one function that runs discovery —
 * the same split probe keeps between its decision and ffmpeg, so the parts a
 * TV is picky about are tested in milliseconds without a TV.
 */
package dlna

import (
	"bufio"
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// The UPnP types this server is and offers.
const (
	DeviceType        = "urn:schemas-upnp-org:device:MediaServer:1"
	ContentDirectory  = "urn:schemas-upnp-org:service:ContentDirectory:1"
	ConnectionManager = "urn:schemas-upnp-org:service:ConnectionManager:1"
	rootDevice        = "upnp:rootdevice"

	// MulticastAddr is where SSDP lives.
	MulticastAddr = "239.255.255.250:1900"
	// MaxAge is how long an announcement is good for, in seconds. Renewed at
	// a third of this, so a TV that misses one announcement still keeps it.
	MaxAge = 1800
)

// Device is what discovery says about this server.
type Device struct {
	// UUID is stable across restarts, derived from the server's identity, so
	// a TV that remembers the server finds the same one again.
	UUID string
	// Location is the URL of the device description on the DLNA listener.
	Location string
	// Server is the SERVER header: OS, UPnP version, product.
	Server string
}

// targets are every search target this device answers to, in the order a
// notify burst sends them.
func (d Device) targets() []string {
	return []string{rootDevice, "uuid:" + d.UUID, DeviceType, ContentDirectory, ConnectionManager}
}

// usn is the unique service name for one target.
func (d Device) usn(target string) string {
	if target == "uuid:"+d.UUID {
		return target
	}
	return "uuid:" + d.UUID + "::" + target
}

// Search is a parsed M-SEARCH request.
type Search struct {
	// Target is the ST header: what the searcher is looking for.
	Target string
}

// ParseSearch reads an SSDP datagram and reports whether it is a discovery
// search. Anything else on the multicast group — another server's NOTIFY, a
// malformed packet — is not an error, just not a search.
func ParseSearch(packet []byte) (Search, bool) {
	req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(packet)))
	if err != nil || req.Method != "M-SEARCH" {
		return Search{}, false
	}
	if strings.Trim(req.Header.Get("MAN"), `"`) != "ssdp:discover" {
		return Search{}, false
	}
	st := strings.TrimSpace(req.Header.Get("ST"))
	if st == "" {
		return Search{}, false
	}
	return Search{Target: st}, true
}

// Answers lists the targets a search should be answered with: every one for
// ssdp:all, the one asked for if this device offers it, and none otherwise.
func (d Device) Answers(s Search) []string {
	if s.Target == "ssdp:all" {
		return d.targets()
	}
	for _, t := range d.targets() {
		if strings.EqualFold(t, s.Target) {
			return []string{t}
		}
	}
	return nil
}

// SearchResponse is the unicast reply to a search, for one target.
func (d Device) SearchResponse(target string) []byte {
	return []byte(strings.Join([]string{
		"HTTP/1.1 200 OK",
		"CACHE-CONTROL: max-age=" + strconv.Itoa(MaxAge),
		"EXT:",
		"LOCATION: " + d.Location,
		"SERVER: " + d.Server,
		"ST: " + target,
		"USN: " + d.usn(target),
		"", "",
	}, "\r\n"))
}

// Notify is an announcement for one target: alive when starting and on a
// timer, byebye when stopping, so a TV drops the server at once rather than
// when its cache expires.
func (d Device) Notify(target string, alive bool) []byte {
	lines := []string{
		"NOTIFY * HTTP/1.1",
		"HOST: " + MulticastAddr,
		"NT: " + target,
		"USN: " + d.usn(target),
	}
	if alive {
		lines = append(lines,
			"NTS: ssdp:alive",
			"CACHE-CONTROL: max-age="+strconv.Itoa(MaxAge),
			"LOCATION: "+d.Location,
			"SERVER: "+d.Server,
		)
	} else {
		lines = append(lines, "NTS: ssdp:byebye")
	}
	return []byte(strings.Join(append(lines, "", ""), "\r\n"))
}

// Notifies is the whole announcement burst, one message per target.
func (d Device) Notifies(alive bool) [][]byte {
	var out [][]byte
	for _, t := range d.targets() {
		out = append(out, d.Notify(t, alive))
	}
	return out
}

// UUIDFrom formats sixteen bytes of a stable identity as a UUID, version and
// variant bits set (a name-based UUID), so the same server is always the same
// device.
func UUIDFrom(b [16]byte) string {
	b[6] = (b[6] & 0x0f) | 0x50
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
