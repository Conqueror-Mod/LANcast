/*
Package diskuse measures what LANcast keeps on disk (settings-3-plan step 7).

One walk of the data folder, with every file counted against the part it
belongs to — the database, artwork, scrub-bar previews, backups — by the name
of the top-level entry it sits under. Anything nobody claims is "other", and
its largest entries are named, because "other: 90 MB" is a question while
"lancast.db.moved-20261019: 44 MB" is an answer.

Folders that may live outside the data folder (optimized copies, ADR 0087)
are walked on their own, so a copy is never counted twice and never missed.

Read-only. Nothing here deletes, and nothing follows a link out of the folder
it was asked about: a link is counted as the link, not as what it points at.
*/
package diskuse

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Part is one kind of thing LANcast keeps, and how much of it there is.
type Part struct {
	ID    string `json:"id"`
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	Files int64  `json:"files"`
	// Outside is true for a folder that is not inside the data folder, which
	// may be on another drive.
	Outside bool `json:"outside,omitempty"`
	// Largest names what "other" holds, biggest first. Empty for every other
	// part.
	Largest []Entry `json:"largest,omitempty"`
}

// Entry is one top-level file or folder and its size.
type Entry struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
}

// Report is one measurement.
type Report struct {
	DataDir    string `json:"data_dir"`
	Parts      []Part `json:"parts"`
	TotalBytes int64  `json:"total_bytes"`
	MeasuredAt int64  `json:"measured_at"`
	TookMS     int64  `json:"took_ms"`
	// Disk is the drive holding the data folder: free and total bytes, or
	// zero where the system would not say.
	DiskFree  uint64 `json:"disk_free_bytes"`
	DiskTotal uint64 `json:"disk_total_bytes"`
	// Partial is true when the walk was stopped before it finished, so the
	// numbers are a lower bound.
	Partial bool `json:"partial,omitempty"`
}

// Layout says which top-level entries of the data folder belong to which part.
type Layout struct {
	DataDir string
	// ByName maps a top-level name in the data folder to a part id.
	ByName map[string]string
	// ByPrefix maps a top-level name prefix to a part id, for families of
	// files such as rotated logs. Checked after ByName.
	ByPrefix map[string]string
	// Order is the part ids in the order they are reported. "other" is
	// always last and need not be listed.
	Order []string
	// Elsewhere are parts whose folder is outside the data folder, by id.
	Elsewhere map[string]string
}

const otherID = "other"

// largestShown is how many of "other"'s entries are named.
const largestShown = 5

// Measure walks the layout and reports. A cancelled ctx stops the walk and
// returns what was counted, marked Partial.
func Measure(ctx context.Context, l Layout) Report {
	start := time.Now()
	r := Report{DataDir: l.DataDir}
	parts := map[string]*Part{}
	part := func(id, path string) *Part {
		p, ok := parts[id]
		if !ok {
			p = &Part{ID: id, Path: path}
			parts[id] = p
		}
		return p
	}

	others := map[string]int64{}
	entries, err := os.ReadDir(l.DataDir)
	if err == nil {
		for _, e := range entries {
			if ctx.Err() != nil {
				r.Partial = true
				break
			}
			name := e.Name()
			id := l.partFor(name)
			full := filepath.Join(l.DataDir, name)
			// A part whose folder is one of these entries reports that folder;
			// a part made of several files reports the data folder.
			where := l.DataDir
			if e.IsDir() && id != otherID {
				where = full
			}
			p := part(id, where)
			if p.Path != where {
				p.Path = l.DataDir
			}
			bytes, files, stopped := walk(ctx, full, e)
			p.Bytes += bytes
			p.Files += files
			if id == otherID {
				others[name] += bytes
			}
			if stopped {
				r.Partial = true
				break
			}
		}
	}

	for id, dir := range l.Elsewhere {
		if ctx.Err() != nil {
			r.Partial = true
			break
		}
		if inside(l.DataDir, dir) {
			continue // counted by the walk above, under its own name
		}
		p := part(id, dir)
		p.Path, p.Outside = dir, true
		info, err := os.Lstat(dir)
		if err != nil {
			continue
		}
		bytes, files, stopped := walk(ctx, dir, fs.FileInfoToDirEntry(info))
		p.Bytes += bytes
		p.Files += files
		if stopped {
			r.Partial = true
		}
	}

	if o, ok := parts[otherID]; ok {
		for name, b := range others {
			o.Largest = append(o.Largest, Entry{Name: name, Bytes: b})
		}
		sort.Slice(o.Largest, func(i, j int) bool {
			if o.Largest[i].Bytes != o.Largest[j].Bytes {
				return o.Largest[i].Bytes > o.Largest[j].Bytes
			}
			return o.Largest[i].Name < o.Largest[j].Name
		})
		if len(o.Largest) > largestShown {
			o.Largest = o.Largest[:largestShown]
		}
	}

	// Every listed part is reported, at zero if it is not there: "previews:
	// nothing yet" is an answer, a missing row is a question.
	for _, id := range append(append([]string{}, l.Order...), otherID) {
		p, ok := parts[id]
		if !ok {
			path := l.DataDir
			if dir, ok := l.Elsewhere[id]; ok {
				path = dir
			}
			p = &Part{ID: id, Path: path, Outside: l.Elsewhere[id] != "" && !inside(l.DataDir, l.Elsewhere[id])}
		}
		r.Parts = append(r.Parts, *p)
		r.TotalBytes += p.Bytes
		delete(parts, id)
	}

	r.DiskFree, r.DiskTotal = diskSpace(l.DataDir)
	r.MeasuredAt = time.Now().Unix()
	r.TookMS = time.Since(start).Milliseconds()
	return r
}

func (l Layout) partFor(name string) string {
	if id, ok := l.ByName[name]; ok {
		return id
	}
	// Longest prefix wins, so "lancast.db-wal" is never claimed by "lancast".
	best, bestLen := otherID, 0
	for prefix, id := range l.ByPrefix {
		if strings.HasPrefix(name, prefix) && len(prefix) > bestLen {
			best, bestLen = id, len(prefix)
		}
	}
	return best
}

// walk counts the bytes and files under one entry. A link is counted as
// itself: following it could count a library drive as LANcast's own.
func walk(ctx context.Context, path string, e fs.DirEntry) (bytes, files int64, stopped bool) {
	if !e.IsDir() {
		if info, err := e.Info(); err == nil {
			return info.Size(), 1, false
		}
		return 0, 0, false
	}
	n := 0
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable folder is skipped, not fatal: the rest is still
			// worth reporting.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if n++; n%1024 == 0 && ctx.Err() != nil {
			stopped = true
			return fs.SkipAll
		}
		if d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			bytes += info.Size()
			files++
		}
		return nil
	})
	return bytes, files, stopped
}

// inside reports whether path is dir or under it, compared lexically after
// cleaning and case-folding, which is how Windows compares paths.
func inside(dir, path string) bool {
	rel, err := filepath.Rel(strings.ToLower(filepath.Clean(dir)), strings.ToLower(filepath.Clean(path)))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
