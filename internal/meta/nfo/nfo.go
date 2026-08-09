// Package nfo reads and writes Kodi-style .nfo sidecar files.
//
// Reading them is the migration path from Kodi; writing them keeps a library
// portable and readable by Kodi, Jellyfin, and Emby — the practical expression
// of "your data is yours". The metadata lives in your folders, not locked in
// LANcast's database.
//
// The hazard this package exists to handle is in ADR 0009: LANcast writes an
// NFO, then reads its own output back as an authoritative local source,
// permanently outranking the provider and silently freezing the item. Every
// file written carries a hash marker so its own output is recognizable on the
// way back in.
package nfo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"lancast/internal/meta"
)

// ID is the local-source identifier.
const ID = "nfo"

// MarkerElement is the provenance element LANcast writes into files it
// generates.
const MarkerElement = "lancast"

// Source reads NFO sidecars. It implements meta.LocalSource.
type Source struct{}

func New() *Source { return &Source{} }

func (s *Source) ID() string { return ID }

// Read returns the metadata in the sidecar beside path, or nil if there is
// none to honor.
//
// A file LANcast wrote and nobody has touched is a *mirror* — a cache of our
// own output, not a statement about the library — so it returns nil and lets
// provider updates proceed. A file a human or another tool edited is
// authoritative and outranks the provider.
func (s *Source) Read(ctx context.Context, path string, kind meta.Kind) (*meta.Record, error) {
	file, ok := findSidecar(path, kind)
	if !ok {
		return nil, nil
	}

	raw, err := os.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("nfo: read %s: %w", file, err)
	}

	var root node
	if err := xml.Unmarshal(raw, &root); err != nil {
		// A malformed sidecar is the user's file, not our bug. Ignore it rather
		// than failing the whole enrichment run.
		return nil, nil
	}

	rec := recordFrom(&root, kind)
	if rec == nil {
		return nil, nil
	}

	// Mirror detection: our own output says nothing new.
	//
	// A user edit is only *proven* when the digest is one this build can
	// compute and it differs. Anything else — a version we do not know, a
	// malformed value — means we cannot tell, and an unverifiable file carrying
	// our own marker is treated as ours.
	if marker, found := root.child(MarkerElement); found {
		if !provesUserEdit(marker.attr("hash"), rec) {
			return nil, nil
		}
		// Something changed. With per-field digests we can say what, and return
		// only that — so correcting a title does not also promote the plot and
		// cast sitting beside it, which is how one fix became four.
		if written := decodeDigests(marker.attr("fields")); len(written) > 0 {
			edited := editedFields(rec, written)
			if edited == nil {
				return nil, nil
			}
			edited.Source = ID
			return edited, nil
		}
		// No per-field information: a marker written before this existed. The
		// whole file is authoritative, which is the older behaviour and stays
		// correct — just blunter.
	}

	rec.Source = ID
	return rec, nil
}

// Write saves a record as a sidecar beside path, preserving any elements other
// tools put there. LANcast is a guest in a file format it did not invent.
func (s *Source) Write(path string, kind meta.Kind, rec *meta.Record) error {
	target := sidecarPath(path, kind)

	root := &node{XMLName: xml.Name{Local: rootElement(kind)}}
	if raw, err := os.ReadFile(target); err == nil {
		var existing node
		if xml.Unmarshal(raw, &existing) == nil {
			root = &existing
		}
	}

	applyRecord(root, rec, kind)

	// The marker must be stamped with the hash of what we are about to write,
	// computed by the same function Read uses. Two implementations that drift
	// would make every file look edited and re-freeze items.
	root.removeAll(MarkerElement)
	root.Nodes = append(root.Nodes, node{
		XMLName: xml.Name{Local: MarkerElement},
		Attrs: []xml.Attr{
			{Name: xml.Name{Local: "generated"}, Value: time.Now().UTC().Format(time.RFC3339)},
			{Name: xml.Name{Local: "hash"}, Value: FieldsHash(rec)},
			// Per-field, so a later read can tell which value a person changed
			// rather than only that the file differs.
			{Name: xml.Name{Local: "fields"}, Value: encodeDigests(FieldDigests(rec))},
		},
	})

	// Drop the indentation the parser handed back as text before re-indenting,
	// or every write bakes the previous write's formatting into the file as
	// data. See dropLayoutText: this compounds, and a real sidecar had grown
	// from 10 escaped newlines to 16 in a single rewrite.
	root.dropLayoutText()

	out, err := xml.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("nfo: marshal: %w", err)
	}
	body := append([]byte(xml.Header), out...)
	body = append(body, '\n')

	return writeAtomic(target, body)
}

// writeAtomic writes via a temp file and rename so a crash never leaves a
// partial file over good data.
func writeAtomic(target string, body []byte) error {
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".lancast-*.tmp")
	if err != nil {
		// A read-only mount is not an error worth surfacing — many libraries
		// are read-only by design and NFO writing is opt-in anyway.
		if os.IsPermission(err) {
			return nil
		}
		return fmt.Errorf("nfo: temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("nfo: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("nfo: close: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("nfo: rename: %w", err)
	}
	return nil
}

// hashVersion identifies the scheme FieldsHash uses.
//
// It is written into the marker and checked on the way back in, because the set
// of fields hashed here is not fixed forever. Add one field to the list below
// and every sidecar LANcast has ever written stops matching its own hash — at
// which point each one looks like a file a human edited, and its contents are
// promoted to authority over every provider. Silently, on every machine, all at
// once.
//
// So the version travels with the digest. A digest this build cannot verify is
// treated as *ours* rather than as a user's edit: being wrong that way means
// ignoring an edit, which the user can redo and which locks the field when they
// do. Being wrong the other way re-pins identities to stale files and is the
// failure this whole mechanism exists to prevent.
const hashVersion = 2

// FieldsHash is the canonical digest of the fields LANcast writes.
//
// Read and Write MUST both use this one function. If the two paths ever
// compute the hash differently, every file looks edited, every item becomes
// pinned to its sidecar, and metadata silently stops updating — the exact
// failure ADR 0009 exists to prevent.
func FieldsHash(rec *meta.Record) string {
	if rec == nil {
		return ""
	}
	var b strings.Builder
	for _, name := range hashedFields {
		b.WriteString(name)
		b.WriteByte('=')
		b.WriteString(canonicalField(rec, name))
		b.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return fmt.Sprintf("sha256:v%d:%s", hashVersion, hex.EncodeToString(sum[:]))
}

// recordDigest is the bare whole-record digest, without the scheme label.
//
// Split out because the label changes when the marker gains information — v2
// added per-field digests — while the digest itself did not. Verifying a v1
// marker with a v2 build has to still work: treating every older sidecar as
// unverifiable would silently stop honouring edits on every file already on
// disk, which is a worse bug than the one versioning was added to prevent.
func recordDigest(rec *meta.Record) string {
	full := FieldsHash(rec)
	_, digest, _ := strings.Cut(strings.TrimPrefix(full, "sha256:"), ":")
	return digest
}

// hashedFields is the set covered by the marker, in a fixed order because the
// whole-record digest is built by concatenation.
var hashedFields = []string{
	"title", "year", "overview", "rating", "content_rating",
	"released_at", "duration_ms", "series", "season", "episode",
	"genres", "credits",
}

// canonicalField renders one field to the exact string both digests are
// computed from. One function so the per-field and whole-record hashes cannot
// disagree about what a value is — the failure ADR 0009 names, one level down.
func canonicalField(rec *meta.Record, name string) string {
	f := rec.Fields
	switch name {
	case "title":
		return deref(f.Title)
	case "year":
		return intStr(f.Year)
	case "overview":
		return deref(f.Overview)
	case "rating":
		return floatStr(f.Rating)
	case "content_rating":
		return deref(f.ContentRating)
	case "released_at":
		return int64Str(f.ReleasedAt)
	case "duration_ms":
		return int64Str(f.DurationMS)
	case "series":
		return deref(f.Series)
	case "season":
		return intStr(f.Season)
	case "episode":
		return intStr(f.Episode)
	case "genres":
		genres := append([]string(nil), rec.Genres...)
		sort.Strings(genres)
		return strings.Join(genres, "|")
	case "credits":
		credits := make([]string, 0, len(rec.Credits))
		for _, c := range rec.Credits {
			credits = append(credits, c.Role+":"+c.Name+":"+c.Character)
		}
		sort.Strings(credits)
		return strings.Join(credits, "|")
	}
	return ""
}

// FieldDigests is the per-field half of the marker: what LANcast wrote for each
// field, individually.
//
// The whole-record hash answers "did anything change". This answers "what
// changed", which is the difference between honouring a title correction and
// promoting the plot, cast and rating that happened to sit beside it.
//
// Sixty-four bits per field. A collision would mean reading an edited field as
// unchanged, so a provider could overwrite it — mild, and vanishingly unlikely
// against values a person typed.
func FieldDigests(rec *meta.Record) map[string]string {
	if rec == nil {
		return nil
	}
	out := make(map[string]string, len(hashedFields))
	for _, name := range hashedFields {
		sum := sha256.Sum256([]byte(name + "=" + canonicalField(rec, name)))
		out[name] = hex.EncodeToString(sum[:8])
	}
	return out
}

// encodeDigests renders the per-field digests for the marker attribute, sorted
// so the file is stable between writes that change nothing.
func encodeDigests(d map[string]string) string {
	names := make([]string, 0, len(d))
	for k := range d {
		names = append(names, k)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, k := range names {
		parts = append(parts, k+":"+d[k])
	}
	return strings.Join(parts, ",")
}

// decodeDigests parses the attribute back. A malformed entry is skipped rather
// than failing the read: an unreadable digest means that field cannot be proven
// unchanged, which lands on the safe side — it is treated as ours.
func decodeDigests(attr string) map[string]string {
	if attr == "" {
		return nil
	}
	out := map[string]string{}
	for _, part := range strings.Split(attr, ",") {
		name, digest, ok := strings.Cut(part, ":")
		if !ok || name == "" || digest == "" {
			continue
		}
		out[name] = digest
	}
	return out
}

// editedFields returns a record holding only the fields a person changed since
// LANcast wrote the file, or nil when nothing was.
//
// This is what makes a title correction cost only the title. Everything whose
// digest still matches what we wrote is our own output being read back, and is
// left absent so providers keep filling it.
func editedFields(file *meta.Record, written map[string]string) *meta.Record {
	edited := &meta.Record{Kind: file.Kind}
	var any bool

	for _, name := range hashedFields {
		was, known := written[name]
		now := FieldDigests(file)[name]
		if known && was == now {
			continue // ours, unchanged
		}
		if !known {
			// A field the marker does not mention. Older marker, or a field
			// added since it was written — either way we cannot claim we wrote
			// this value, so it is not ours to ignore.
			if canonicalField(file, name) == "" {
				continue
			}
		}
		if canonicalField(file, name) == "" {
			// Cleared rather than changed. Treated as no opinion rather than as
			// an instruction to blank the field, because a provider filling an
			// empty title is better than a title deliberately emptied by a
			// parser disagreement.
			continue
		}
		copyField(edited, file, name)
		any = true
	}
	if !any {
		return nil
	}
	return edited
}

// copyField moves one field from the parsed file into the edited record.
func copyField(dst, src *meta.Record, name string) {
	s, d := &src.Fields, &dst.Fields
	switch name {
	case "title":
		d.Title = s.Title
		// Sort title follows the title it is derived from, or a corrected title
		// sorts under its old letter.
		d.SortTitle = s.SortTitle
	case "year":
		d.Year = s.Year
	case "overview":
		d.Overview = s.Overview
	case "rating":
		d.Rating = s.Rating
	case "content_rating":
		d.ContentRating = s.ContentRating
	case "released_at":
		d.ReleasedAt = s.ReleasedAt
	case "duration_ms":
		d.DurationMS = s.DurationMS
	case "series":
		d.Series = s.Series
	case "season":
		d.Season = s.Season
	case "episode":
		d.Episode = s.Episode
	case "genres":
		dst.Genres = src.Genres
	case "credits":
		dst.Credits = src.Credits
	}
}

// ------------------------------------------------------------ file locations

func rootElement(kind meta.Kind) string {
	switch kind {
	case meta.KindShow:
		return "tvshow"
	case meta.KindEpisode:
		return "episodedetails"
	default:
		return "movie"
	}
}

// sidecarPath is where LANcast writes. For shows, path is the show directory.
func sidecarPath(path string, kind meta.Kind) string {
	if kind == meta.KindShow || kind == meta.KindSeason {
		return filepath.Join(path, "tvshow.nfo")
	}
	return strings.TrimSuffix(path, filepath.Ext(path)) + ".nfo"
}

// findSidecar looks for an existing file, accepting the conventions other
// tools use as well as our own.
func findSidecar(path string, kind meta.Kind) (string, bool) {
	var candidates []string
	if kind == meta.KindShow || kind == meta.KindSeason {
		candidates = []string{filepath.Join(path, "tvshow.nfo")}
	} else {
		base := strings.TrimSuffix(path, filepath.Ext(path)) + ".nfo"
		candidates = []string{base, filepath.Join(filepath.Dir(path), "movie.nfo")}
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, true
		}
	}
	return "", false
}

// ------------------------------------------------------------------- parsing

// node is a generic XML element, which is what lets unknown elements written
// by other tools survive a rewrite untouched.
type node struct {
	XMLName xml.Name
	Attrs   []xml.Attr `xml:",any,attr"`
	Content string     `xml:",chardata"`
	Nodes   []node     `xml:",any"`
}

// dropLayoutText clears character data that is only whitespace.
//
// `xml:",chardata"` captures everything between an element's children,
// including the newlines and indentation that MarshalIndent itself produced
// last time. Marshalling that back out writes it as content — and Go escapes
// newlines in character data, so it lands in the file as runs of `&#xA;`.
// Re-indenting then adds a fresh layer around it, so the noise compounds on
// every write: one observed sidecar went from 10 escaped newlines to 16 in a
// single rewrite, and would keep growing on every enrichment pass.
//
// Whitespace-only text carries no information — the values live in the leaf
// elements and MarshalIndent recreates the layout — so dropping it is lossless.
// Text that is not purely whitespace is left exactly as it was: that is a real
// value, and trimming it would edit the user's data on a write that was only
// supposed to reformat.
//
// Existing files repair themselves the next time they are written.
func (n *node) dropLayoutText() {
	if strings.TrimSpace(n.Content) == "" {
		n.Content = ""
	}
	for i := range n.Nodes {
		n.Nodes[i].dropLayoutText()
	}
}

func (n *node) child(name string) (*node, bool) {
	for i := range n.Nodes {
		if strings.EqualFold(n.Nodes[i].XMLName.Local, name) {
			return &n.Nodes[i], true
		}
	}
	return nil, false
}

func (n *node) children(name string) []*node {
	var out []*node
	for i := range n.Nodes {
		if strings.EqualFold(n.Nodes[i].XMLName.Local, name) {
			out = append(out, &n.Nodes[i])
		}
	}
	return out
}

func (n *node) text(name string) string {
	if c, ok := n.child(name); ok {
		return strings.TrimSpace(c.Content)
	}
	return ""
}

func (n *node) attr(name string) string {
	for _, a := range n.Attrs {
		if strings.EqualFold(a.Name.Local, name) {
			return a.Value
		}
	}
	return ""
}

func (n *node) removeAll(name string) {
	kept := n.Nodes[:0]
	for _, c := range n.Nodes {
		if !strings.EqualFold(c.XMLName.Local, name) {
			kept = append(kept, c)
		}
	}
	n.Nodes = kept
}

func (n *node) set(name, value string) {
	if value == "" {
		n.removeAll(name)
		return
	}
	if c, ok := n.child(name); ok {
		c.Content = value
		c.Nodes = nil
		return
	}
	n.Nodes = append(n.Nodes, node{XMLName: xml.Name{Local: name}, Content: value})
}

func recordFrom(root *node, kind meta.Kind) *meta.Record {
	rec := &meta.Record{Kind: kind}
	f := &rec.Fields

	if v := root.text("title"); v != "" {
		f.Title = meta.S(v)
	}
	if v := root.text("sorttitle"); v != "" {
		f.SortTitle = meta.S(v)
	}
	if v := root.text("showtitle"); v != "" {
		f.Series = meta.S(v)
	}
	if v := atoi(root.text("year")); v > 0 {
		f.Year = meta.I(v)
	}
	// Kodi uses <plot>; <outline> is the short form some tools write.
	if v := root.text("plot"); v != "" {
		f.Overview = meta.S(v)
	} else if v := root.text("outline"); v != "" {
		f.Overview = meta.S(v)
	}
	if v := atof(root.text("rating")); v > 0 {
		f.Rating = meta.F(v)
	}
	if v := root.text("mpaa"); v != "" {
		f.ContentRating = meta.S(v)
	}
	if v := atoi(root.text("runtime")); v > 0 {
		f.DurationMS = meta.I64(int64(v) * 60_000)
	}
	if v := atoi(root.text("season")); v > 0 {
		f.Season = meta.I(v)
	}
	if v := atoi(root.text("episode")); v > 0 {
		f.Episode = meta.I(v)
	}

	date := root.text("premiered")
	if date == "" {
		date = root.text("aired")
	}
	if ts, ok := parseDate(date); ok {
		f.ReleasedAt = meta.I64(ts)
		if f.Year == nil && len(date) >= 4 {
			if y := atoi(date[:4]); y > 0 {
				f.Year = meta.I(y)
			}
		}
	}

	for _, g := range root.children("genre") {
		if v := strings.TrimSpace(g.Content); v != "" {
			rec.Genres = append(rec.Genres, v)
		}
	}
	for i, a := range root.children("actor") {
		name := a.text("name")
		if name == "" {
			continue
		}
		rec.Credits = append(rec.Credits, meta.Credit{
			Name: name, Role: "actor", Character: a.text("role"), Order: i,
		})
	}
	for _, d := range root.children("director") {
		if v := strings.TrimSpace(d.Content); v != "" {
			rec.Credits = append(rec.Credits, meta.Credit{Name: v, Role: "director"})
		}
	}

	// A sidecar with nothing usable is the same as no sidecar.
	if f.Title == nil && f.Overview == nil && len(rec.Genres) == 0 && len(rec.Credits) == 0 {
		return nil
	}
	return rec
}

func applyRecord(root *node, rec *meta.Record, kind meta.Kind) {
	if rec == nil {
		return
	}
	f := rec.Fields

	root.set("title", deref(f.Title))
	root.set("sorttitle", deref(f.SortTitle))
	root.set("plot", deref(f.Overview))
	root.set("mpaa", deref(f.ContentRating))
	if f.Year != nil {
		root.set("year", strconv.Itoa(*f.Year))
	}
	if f.Rating != nil {
		root.set("rating", floatStr(f.Rating))
	}
	if f.DurationMS != nil {
		root.set("runtime", strconv.FormatInt(*f.DurationMS/60_000, 10))
	}
	if f.ReleasedAt != nil {
		date := time.Unix(*f.ReleasedAt, 0).UTC().Format("2006-01-02")
		if kind == meta.KindEpisode {
			root.set("aired", date)
		} else {
			root.set("premiered", date)
		}
	}
	if kind == meta.KindEpisode {
		root.set("showtitle", deref(f.Series))
		if f.Season != nil {
			root.set("season", strconv.Itoa(*f.Season))
		}
		if f.Episode != nil {
			root.set("episode", strconv.Itoa(*f.Episode))
		}
	}

	root.removeAll("genre")
	for _, g := range rec.Genres {
		root.Nodes = append(root.Nodes, node{XMLName: xml.Name{Local: "genre"}, Content: g})
	}

	root.removeAll("actor")
	root.removeAll("director")
	for _, c := range rec.Credits {
		switch c.Role {
		case "actor":
			root.Nodes = append(root.Nodes, node{
				XMLName: xml.Name{Local: "actor"},
				Nodes: []node{
					{XMLName: xml.Name{Local: "name"}, Content: c.Name},
					{XMLName: xml.Name{Local: "role"}, Content: c.Character},
				},
			})
		case "director":
			root.Nodes = append(root.Nodes, node{XMLName: xml.Name{Local: "director"}, Content: c.Name})
		}
	}
}

// ------------------------------------------------------------------- helpers

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func intStr(p *int) string {
	if p == nil {
		return ""
	}
	return strconv.Itoa(*p)
}

func int64Str(p *int64) string {
	if p == nil {
		return ""
	}
	return strconv.FormatInt(*p, 10)
}

// floatStr fixes the precision so the hash is stable across a round trip.
func floatStr(p *float64) string {
	if p == nil {
		return ""
	}
	return strconv.FormatFloat(*p, 'f', 1, 64)
}

func atoi(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

func atof(s string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return f
}

func parseDate(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if len(s) < 10 {
		return 0, false
	}
	t, err := time.Parse("2006-01-02", s[:10])
	if err != nil {
		return 0, false
	}
	return t.Unix(), true
}

// provesUserEdit reports whether a marker hash demonstrates that someone changed
// the file after LANcast wrote it.
//
// It answers "can we prove an edit", not "did the hash match", and the
// difference is the whole point: an unrecognised scheme is not evidence of a
// human, it is evidence that this build cannot check.
func provesUserEdit(marker string, rec *meta.Record) bool {
	version, ok := hashSchemeOf(marker)
	if !ok || version > hashVersion {
		// Either not one of ours to parse, or written by a build newer than
		// this one. Not proof of anything.
		return false
	}
	// Every scheme so far digests the record identically; only the marker's
	// shape has grown. So a v1 marker is still verifiable here, and edits made
	// to files written before per-field digests existed keep being honoured.
	_, digest, hasVer := strings.Cut(strings.TrimPrefix(marker, "sha256:"), ":")
	if !hasVer {
		digest = strings.TrimPrefix(marker, "sha256:")
	}
	return digest != recordDigest(rec)
}

// hashSchemeOf extracts the scheme version from a marker hash.
//
// "sha256:v1:<hex>" is the current form. "sha256:<hex>" is what builds before
// versioning wrote, and it is version 1 — those files were produced by exactly
// the scheme this constant now names, so reading them as v1 is a statement of
// fact rather than an assumption.
func hashSchemeOf(marker string) (int, bool) {
	rest, ok := strings.CutPrefix(marker, "sha256:")
	if !ok {
		return 0, false
	}
	verPart, digest, hasVersion := strings.Cut(rest, ":")
	if !hasVersion {
		// Unversioned: the pre-versioning form.
		return 1, rest != ""
	}
	if digest == "" || !strings.HasPrefix(verPart, "v") {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(verPart, "v"))
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}
