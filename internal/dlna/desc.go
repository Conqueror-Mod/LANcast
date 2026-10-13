package dlna

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// esc escapes text for XML. Titles are anything a filename or a provider said,
// so ampersands and angle brackets are ordinary.
func esc(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

/*
 * DeviceDescription is the document LOCATION points at: who this server is
 * and which services it offers.
 *
 * The X_DLNADOC element is what several TVs (Samsung and Sony among them)
 * look for before treating a UPnP server as a media server at all.
 */
func DeviceDescription(name, uuid string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<root xmlns="urn:schemas-upnp-org:device-1-0" xmlns:dlna="urn:schemas-dlna-org:device-1-0">
  <specVersion><major>1</major><minor>0</minor></specVersion>
  <device>
    <deviceType>` + DeviceType + `</deviceType>
    <friendlyName>` + esc(name) + `</friendlyName>
    <manufacturer>LANcast</manufacturer>
    <modelName>LANcast</modelName>
    <modelDescription>LANcast media server</modelDescription>
    <UDN>uuid:` + uuid + `</UDN>
    <dlna:X_DLNADOC>DMS-1.50</dlna:X_DLNADOC>
    <serviceList>
      <service>
        <serviceType>` + ContentDirectory + `</serviceType>
        <serviceId>urn:upnp-org:serviceId:ContentDirectory</serviceId>
        <SCPDURL>/dlna/cds.xml</SCPDURL>
        <controlURL>/dlna/control/cds</controlURL>
        <eventSubURL>/dlna/event/cds</eventSubURL>
      </service>
      <service>
        <serviceType>` + ConnectionManager + `</serviceType>
        <serviceId>urn:upnp-org:serviceId:ConnectionManager</serviceId>
        <SCPDURL>/dlna/cms.xml</SCPDURL>
        <controlURL>/dlna/control/cms</controlURL>
        <eventSubURL>/dlna/event/cms</eventSubURL>
      </service>
    </serviceList>
  </device>
</root>`
}

// ContentDirectorySCPD describes the Browse service: the actions a control
// point may call and their arguments.
const ContentDirectorySCPD = `<?xml version="1.0" encoding="UTF-8"?>
<scpd xmlns="urn:schemas-upnp-org:service-1-0">
  <specVersion><major>1</major><minor>0</minor></specVersion>
  <actionList>
    <action><name>Browse</name><argumentList>
      <argument><name>ObjectID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_ObjectID</relatedStateVariable></argument>
      <argument><name>BrowseFlag</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_BrowseFlag</relatedStateVariable></argument>
      <argument><name>Filter</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Filter</relatedStateVariable></argument>
      <argument><name>StartingIndex</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Index</relatedStateVariable></argument>
      <argument><name>RequestedCount</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Count</relatedStateVariable></argument>
      <argument><name>SortCriteria</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_SortCriteria</relatedStateVariable></argument>
      <argument><name>Result</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_Result</relatedStateVariable></argument>
      <argument><name>NumberReturned</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_Count</relatedStateVariable></argument>
      <argument><name>TotalMatches</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_Count</relatedStateVariable></argument>
      <argument><name>UpdateID</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_UpdateID</relatedStateVariable></argument>
    </argumentList></action>
    <action><name>GetSystemUpdateID</name><argumentList>
      <argument><name>Id</name><direction>out</direction><relatedStateVariable>SystemUpdateID</relatedStateVariable></argument>
    </argumentList></action>
    <action><name>GetSearchCapabilities</name><argumentList>
      <argument><name>SearchCaps</name><direction>out</direction><relatedStateVariable>SearchCapabilities</relatedStateVariable></argument>
    </argumentList></action>
    <action><name>GetSortCapabilities</name><argumentList>
      <argument><name>SortCaps</name><direction>out</direction><relatedStateVariable>SortCapabilities</relatedStateVariable></argument>
    </argumentList></action>
  </actionList>
  <serviceStateTable>
    <stateVariable sendEvents="no"><name>A_ARG_TYPE_ObjectID</name><dataType>string</dataType></stateVariable>
    <stateVariable sendEvents="no"><name>A_ARG_TYPE_BrowseFlag</name><dataType>string</dataType>
      <allowedValueList><allowedValue>BrowseMetadata</allowedValue><allowedValue>BrowseDirectChildren</allowedValue></allowedValueList></stateVariable>
    <stateVariable sendEvents="no"><name>A_ARG_TYPE_Filter</name><dataType>string</dataType></stateVariable>
    <stateVariable sendEvents="no"><name>A_ARG_TYPE_Index</name><dataType>ui4</dataType></stateVariable>
    <stateVariable sendEvents="no"><name>A_ARG_TYPE_Count</name><dataType>ui4</dataType></stateVariable>
    <stateVariable sendEvents="no"><name>A_ARG_TYPE_SortCriteria</name><dataType>string</dataType></stateVariable>
    <stateVariable sendEvents="no"><name>A_ARG_TYPE_Result</name><dataType>string</dataType></stateVariable>
    <stateVariable sendEvents="no"><name>A_ARG_TYPE_UpdateID</name><dataType>ui4</dataType></stateVariable>
    <stateVariable sendEvents="yes"><name>SystemUpdateID</name><dataType>ui4</dataType></stateVariable>
    <stateVariable sendEvents="no"><name>SearchCapabilities</name><dataType>string</dataType></stateVariable>
    <stateVariable sendEvents="no"><name>SortCapabilities</name><dataType>string</dataType></stateVariable>
  </serviceStateTable>
</scpd>`

// ConnectionManagerSCPD is the minimum a control point checks for: what the
// server can send.
const ConnectionManagerSCPD = `<?xml version="1.0" encoding="UTF-8"?>
<scpd xmlns="urn:schemas-upnp-org:service-1-0">
  <specVersion><major>1</major><minor>0</minor></specVersion>
  <actionList>
    <action><name>GetProtocolInfo</name><argumentList>
      <argument><name>Source</name><direction>out</direction><relatedStateVariable>SourceProtocolInfo</relatedStateVariable></argument>
      <argument><name>Sink</name><direction>out</direction><relatedStateVariable>SinkProtocolInfo</relatedStateVariable></argument>
    </argumentList></action>
    <action><name>GetCurrentConnectionIDs</name><argumentList>
      <argument><name>ConnectionIDs</name><direction>out</direction><relatedStateVariable>CurrentConnectionIDs</relatedStateVariable></argument>
    </argumentList></action>
  </actionList>
  <serviceStateTable>
    <stateVariable sendEvents="yes"><name>SourceProtocolInfo</name><dataType>string</dataType></stateVariable>
    <stateVariable sendEvents="yes"><name>SinkProtocolInfo</name><dataType>string</dataType></stateVariable>
    <stateVariable sendEvents="yes"><name>CurrentConnectionIDs</name><dataType>string</dataType></stateVariable>
  </serviceStateTable>
</scpd>`

// Object is one entry in a Browse result: a folder or a playable item.
type Object struct {
	ID, ParentID string
	Title        string
	// Class is the UPnP class, e.g. object.item.videoItem.movie.
	Class     string
	Container bool
	// Res is the playable file; empty for a container.
	Res *Resource
	// ArtURL is a poster or cover, when there is one.
	ArtURL string
	Year   int
}

// Resource is a file a renderer can fetch.
type Resource struct {
	URL      string
	MIME     string
	Size     int64
	Duration string // H:MM:SS.000, empty when unknown
}

// DIDL renders objects as DIDL-Lite, the XML a Browse result carries.
func DIDL(objs []Object) string {
	var b strings.Builder
	b.WriteString(`<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" ` +
		`xmlns:dc="http://purl.org/dc/elements/1.1/" ` +
		`xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/" ` +
		`xmlns:dlna="urn:schemas-dlna-org:metadata-1-0/">`)
	for _, o := range objs {
		tag := "item"
		if o.Container {
			tag = "container"
		}
		fmt.Fprintf(&b, `<%s id="%s" parentID="%s" restricted="1">`, tag, esc(o.ID), esc(o.ParentID))
		fmt.Fprintf(&b, `<dc:title>%s</dc:title><upnp:class>%s</upnp:class>`, esc(o.Title), esc(o.Class))
		if o.Year > 0 {
			fmt.Fprintf(&b, `<dc:date>%04d-01-01</dc:date>`, o.Year)
		}
		if o.ArtURL != "" {
			fmt.Fprintf(&b, `<upnp:albumArtURI dlna:profileID="JPEG_TN">%s</upnp:albumArtURI>`, esc(o.ArtURL))
		}
		if r := o.Res; r != nil {
			b.WriteString(`<res protocolInfo="http-get:*:` + esc(r.MIME) + `:*"`)
			if r.Size > 0 {
				fmt.Fprintf(&b, ` size="%d"`, r.Size)
			}
			if r.Duration != "" {
				b.WriteString(` duration="` + r.Duration + `"`)
			}
			b.WriteString(`>` + esc(r.URL) + `</res>`)
		}
		b.WriteString(`</` + tag + `>`)
	}
	b.WriteString(`</DIDL-Lite>`)
	return b.String()
}

// Duration formats milliseconds as DLNA wants them: H:MM:SS.mmm.
func Duration(ms int64) string {
	if ms <= 0 {
		return ""
	}
	s := ms / 1000
	return fmt.Sprintf("%d:%02d:%02d.%03d", s/3600, s/60%60, s%60, ms%1000)
}

// MIME guesses a file's type from its container, which is what a renderer
// decides "can I play this" by. Unknown containers get octet-stream: listed,
// and left for the renderer to refuse.
func MIME(kind, container string) string {
	switch strings.ToLower(container) {
	case "mkv", "matroska", "matroska,webm":
		return "video/x-matroska"
	case "mp4", "m4v", "mov,mp4,m4a,3gp,3g2,mj2":
		return "video/mp4"
	case "avi":
		return "video/x-msvideo"
	case "ts", "mpegts", "m2ts":
		return "video/mp2t"
	case "webm":
		return "video/webm"
	case "mp3":
		return "audio/mpeg"
	case "flac":
		return "audio/flac"
	case "m4a", "aac":
		return "audio/mp4"
	case "ogg", "opus":
		return "audio/ogg"
	case "wav":
		return "audio/wav"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	}
	return "application/octet-stream"
}
