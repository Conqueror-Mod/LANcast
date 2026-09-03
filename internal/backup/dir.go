/*
 * Package backup owns the folder backups live in.
 *
 * The snapshot itself belongs to store (ADR 0058) — this is everything around
 * it: where the files go, what they are called, which of them this build could
 * restore, and the containment rule that stops a name in a URL becoming a path
 * anywhere else on the disk.
 *
 * Separated from the HTTP layer for the reason probe's decision is separated
 * from ffmpeg: the interesting rules here are about strings and paths, and
 * they are worth testing without a server, a request, or a database.
 */
package backup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"lancast/internal/store"
)

// Prefix and Ext are what a backup is called. Named constants because the
// listing recognises its own files by them, and a listing that disagreed with
// the writer would show an empty folder full of backups.
const (
	Prefix = "lancast-backup-"
	Ext    = ".db"
)

// ErrBadName reports a name that is not a backup this folder could hold.
var ErrBadName = errors.New("not a backup name")

// Dir is the folder backups are written to and read from.
type Dir struct{ path string }

// New returns the backup folder inside a data directory. Nothing is created
// until something is written.
func New(dataDir string) *Dir {
	return &Dir{path: filepath.Join(dataDir, "backups")}
}

// Path is the folder itself, which the client shows so somebody can go and
// copy the file somewhere that is not this disk. A backup that only exists on
// the drive it is protecting against is not really a backup, and the surest
// way to say so is to show where it is.
func (d *Dir) Path() string { return d.path }

// File is one backup on disk.
type File struct {
	Name    string `json:"name"`
	Bytes   int64  `json:"bytes"`
	TakenAt int64  `json:"taken_at"`
	// SchemaVersion is the revision recorded inside the file, zero when it
	// could not be read.
	SchemaVersion int `json:"schema_version"`
	// Restorable is whether *this build* could restore it. A backup from a
	// newer LANcast cannot be, and saying so in the list is the difference
	// between finding out now and finding out during a restore.
	Restorable bool `json:"restorable"`
	// Problem is why not, in a sentence, empty when there is none.
	Problem string `json:"problem,omitempty"`
}

/*
 * NewName is what the next backup is called.
 *
 * Built from *local* time components. `Format` on a local time already does
 * that, and it is called out because the UTC version of this mistake has
 * shipped in this project before: a name derived from a UTC date reads as
 * tomorrow's backup all evening, in every US timezone.
 */
func NewName(now time.Time) string {
	return Prefix + now.Format("20060102-150405") + Ext
}

/*
 * Resolve turns a name from a request into a path inside this folder.
 *
 * This is the boundary the project's containment rule is about: a name that
 * arrives from outside becomes a filesystem path here, and every way of
 * spelling "somewhere else" has to die at this line rather than further in.
 * The name is required to be a bare filename with the expected shape, and the
 * resolved path is then re-verified to sit inside the folder after Abs — the
 * check is done twice on purpose, because the first is about what was asked
 * for and the second is about what it turned into.
 */
func (d *Dir) Resolve(name string) (string, error) {
	if name == "" || name != filepath.Base(name) || strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("%q: %w", name, ErrBadName)
	}
	if name == "." || name == ".." {
		return "", fmt.Errorf("%q: %w", name, ErrBadName)
	}
	if !strings.HasPrefix(name, Prefix) || !strings.HasSuffix(name, Ext) {
		return "", fmt.Errorf("%q: %w", name, ErrBadName)
	}

	root, err := filepath.Abs(d.path)
	if err != nil {
		return "", err
	}
	full, err := filepath.Abs(filepath.Join(root, name))
	if err != nil {
		return "", err
	}
	if filepath.Dir(full) != root {
		return "", fmt.Errorf("%q: %w", name, ErrBadName)
	}
	return full, nil
}

// Create makes the folder if it is not there yet. Called before writing rather
// than at startup: a server nobody has ever taken a backup on should not have
// an empty folder implying they did.
func (d *Dir) Create() error {
	if err := os.MkdirAll(d.path, 0o700); err != nil {
		return fmt.Errorf("backup folder %s: %w", d.path, err)
	}
	return nil
}

/*
 * List returns the backups, newest first.
 *
 * Each file is opened to read the revision inside it, which is what decides
 * whether this build could restore it. That is one row from a read-only
 * handle, and it is worth the cost: the alternative is a list that looks
 * uniform and hides the one entry that cannot be used.
 *
 * A file that cannot be read is *listed*, not skipped. A backup that has gone
 * bad is the single most important thing this screen can tell somebody, and a
 * list that quietly omits it says the opposite of the truth.
 */
func (d *Dir) List() ([]File, error) {
	entries, err := os.ReadDir(d.path)
	if errors.Is(err, os.ErrNotExist) {
		// Nobody has taken one yet. An empty list, not an error.
		return []File{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read backup folder %s: %w", d.path, err)
	}

	out := make([]File, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, Prefix) || !strings.HasSuffix(name, Ext) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}

		f := File{Name: name, Bytes: info.Size(), TakenAt: info.ModTime().Unix()}
		snap, err := store.InspectSnapshot(filepath.Join(d.path, name))
		switch {
		case err == nil:
			f.SchemaVersion = snap.SchemaVersion
			f.Restorable = true
		case errors.Is(err, store.ErrNotSnapshot):
			f.Problem = "this file is not a readable LANcast backup"
		default:
			var tooNew *store.SnapshotTooNewError
			if errors.As(err, &tooNew) {
				f.SchemaVersion = tooNew.Found
				f.Problem = "taken by a newer LANcast — update before restoring it"
			} else {
				f.Problem = "could not be read"
			}
		}
		out = append(out, f)
	}

	// Newest first: the one somebody wants is almost always the last one
	// taken. Ties break on name, which is itself a timestamp, so the order is
	// total and a test can rely on it.
	sort.Slice(out, func(i, j int) bool {
		if out[i].TakenAt != out[j].TakenAt {
			return out[i].TakenAt > out[j].TakenAt
		}
		return out[i].Name > out[j].Name
	})
	return out, nil
}

// Remove deletes one backup.
func (d *Dir) Remove(name string) error {
	full, err := d.Resolve(name)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil {
		return fmt.Errorf("delete backup %s: %w", name, err)
	}
	return nil
}
