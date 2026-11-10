package nfo

import (
	"encoding/xml"
	"testing"

	"lancast/internal/meta"
)

/*
 * FuzzRecordFrom feeds the NFO reader a sidecar's bytes, as Read does once
 * the file is found (docs/hardening-plan.md, item 3). NFOs are often written
 * by other tools, and a library may hold thousands.
 *
 * The decode is encoding/xml's; what is LANcast's own is everything after it:
 * turning the tree into a record, reading the digest marker, and deciding
 * whether a person edited the file. None of it may panic, for any kind.
 */
func FuzzRecordFrom(f *testing.F) {
	for _, s := range []string{
		`<movie><title>Heat</title><year>1995</year><uniqueid type="tmdb" default="true">949</uniqueid><genre>Crime</genre></movie>`,
		`<tvshow><title>Show</title><premiered>2001-02-03</premiered><rating>8.1</rating></tvshow>`,
		`<episodedetails><title>Pilot</title><season>1</season><episode>1</episode><runtime>42</runtime></episodedetails>`,
		`<movie><title>Marked</title><lancast hash="v2:00" fields="title:abc,year:def"/></movie>`,
		`<movie><actor><name>A</name><role>B</role><order>x</order></actor><thumb aspect="poster">u</thumb></movie>`,
		`<movie><ratings><rating name="imdb" max="10"><value>NaN</value></rating></ratings></movie>`,
		`<?xml version="1.0"?><movie/>`,
		`<movie><year>99999999999999999999</year><premiered>0000-00-00</premiered></movie>`,
	} {
		f.Add([]byte(s))
	}
	kinds := []meta.Kind{meta.KindMovie, meta.KindShow, meta.KindEpisode}
	f.Fuzz(func(t *testing.T, raw []byte) {
		var root node
		if xml.Unmarshal(raw, &root) != nil {
			return
		}
		for _, k := range kinds {
			rec := recordFrom(&root, k)
			if rec == nil {
				continue
			}
			_ = FieldsHash(rec)
			_ = FieldDigests(rec)
			// The same steps Read takes when the file carries LANcast's marker.
			if marker, found := root.child(MarkerElement); found {
				_ = provesUserEdit(marker.attr("hash"), rec)
				if written := decodeDigests(marker.attr("fields")); len(written) > 0 {
					_ = editedFields(rec, written)
				}
			}
		}
	})
}
