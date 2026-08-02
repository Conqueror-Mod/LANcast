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

	// Mirror detection: if the marker hash still matches the file's own
	// content, this is LANcast's unmodified output and says nothing new.
	if marker, found := root.child(MarkerElement); found {
		if marker.attr("hash") == FieldsHash(rec) {
			return nil, nil
		}
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
	f := rec.Fields

	write := func(k, v string) {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(v)
		b.WriteByte('\n')
	}

	write("title", deref(f.Title))
	write("year", intStr(f.Year))
	write("overview", deref(f.Overview))
	write("rating", floatStr(f.Rating))
	write("content_rating", deref(f.ContentRating))
	write("released_at", int64Str(f.ReleasedAt))
	write("duration_ms", int64Str(f.DurationMS))
	write("series", deref(f.Series))
	write("season", intStr(f.Season))
	write("episode", intStr(f.Episode))

	genres := append([]string(nil), rec.Genres...)
	sort.Strings(genres)
	write("genres", strings.Join(genres, "|"))

	credits := make([]string, 0, len(rec.Credits))
	for _, c := range rec.Credits {
		credits = append(credits, c.Role+":"+c.Name+":"+c.Character)
	}
	sort.Strings(credits)
	write("credits", strings.Join(credits, "|"))

	sum := sha256.Sum256([]byte(b.String()))
	return "sha256:" + hex.EncodeToString(sum[:])
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
