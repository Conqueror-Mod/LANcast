package dlna

import (
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Browse is a parsed ContentDirectory Browse call.
type Browse struct {
	ObjectID string
	// Metadata is BrowseMetadata (the object itself) rather than
	// BrowseDirectChildren (what it holds).
	Metadata bool
	Start    int
	// Count is how many were asked for; zero means all.
	Count int
}

// envelope reads any SOAP body down to its action element's fields.
type envelope struct {
	Body struct {
		Inner []byte `xml:",innerxml"`
	} `xml:"Body"`
}

// ParseAction reads a SOAP request's action name and its arguments.
func ParseAction(r io.Reader) (string, map[string]string, error) {
	var env envelope
	if err := xml.NewDecoder(io.LimitReader(r, 64<<10)).Decode(&env); err != nil {
		return "", nil, fmt.Errorf("soap: %w", err)
	}
	d := xml.NewDecoder(strings.NewReader(string(env.Body.Inner)))
	var action string
	args := map[string]string{}
	depth := 0
	var field string
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", nil, fmt.Errorf("soap: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth == 1 {
				action = t.Name.Local
			} else if depth == 2 {
				field = t.Name.Local
			}
		case xml.CharData:
			if depth == 2 {
				args[field] += string(t)
			}
		case xml.EndElement:
			depth--
		}
	}
	if action == "" {
		return "", nil, fmt.Errorf("soap: no action")
	}
	return action, args, nil
}

// ParseBrowse turns Browse arguments into a Browse. A missing or negative
// index or count is read as the start and "all", which is what control points
// that leave them out mean.
func ParseBrowse(args map[string]string) Browse {
	b := Browse{ObjectID: strings.TrimSpace(args["ObjectID"])}
	if b.ObjectID == "" {
		b.ObjectID = "0"
	}
	b.Metadata = strings.TrimSpace(args["BrowseFlag"]) == "BrowseMetadata"
	if n, err := strconv.Atoi(strings.TrimSpace(args["StartingIndex"])); err == nil && n > 0 {
		b.Start = n
	}
	if n, err := strconv.Atoi(strings.TrimSpace(args["RequestedCount"])); err == nil && n > 0 {
		b.Count = n
	}
	return b
}

// Response wraps an action's out-arguments in a SOAP envelope. Values are
// escaped here, which is what makes a DIDL-Lite Result travel as text inside
// the XML rather than as markup.
func Response(service, action string, out [][2]string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` +
		`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" ` +
		`s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>`)
	b.WriteString(`<u:` + action + `Response xmlns:u="` + service + `">`)
	for _, kv := range out {
		b.WriteString(`<` + kv[0] + `>` + esc(kv[1]) + `</` + kv[0] + `>`)
	}
	b.WriteString(`</u:` + action + `Response></s:Body></s:Envelope>`)
	return b.String()
}

// Fault is a SOAP error, which is how UPnP says "no such object" (701) or
// "invalid action" (401).
func Fault(code int, desc string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>` +
		`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" ` +
		`s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><s:Fault>` +
		`<faultcode>s:Client</faultcode><faultstring>UPnPError</faultstring><detail>` +
		`<UPnPError xmlns="urn:schemas-upnp-org:control-1-0"><errorCode>` + strconv.Itoa(code) +
		`</errorCode><errorDescription>` + esc(desc) + `</errorDescription></UPnPError>` +
		`</detail></s:Fault></s:Body></s:Envelope>`
}
