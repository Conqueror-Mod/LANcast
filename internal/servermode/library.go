// Package servermode moves a library between the service's folder and the
// person's own, which is what switching LANcast between running as a Windows
// service and running only while the app is open actually is (ADR 0092).
//
// The file operations here are pure Go over two directories, and the service
// is an interface, so every step and every rollback is tested on any platform
// with real files and a real database. The Windows-only parts — the elevated
// helper, the service manager, the permissions grant — live with their callers.
package servermode

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DBName is the database's file name, and the one thing every "which mode is
// this machine in" check looks for (ADR 0092, Context).
const DBName = "lancast.db"

// incomingName is the copy's name until it is proven. Nothing opens it as a
// library, and SQLite derives the sidecar names from it, so the copied WAL
// stays attached to the copied database (ADR 0092 §4).
const incomingName = DBName + ".incoming"

// sidecars are the suffixes SQLite puts beside a WAL database.
var sidecars = []string{"-wal", "-shm"}

/*
 * skip reports whether an entry of a library folder stays behind.
 *
 * rel is slash-separated and relative to the library folder. Only top-level
 * names are judged: everything inside a folder that travels, travels.
 *
 *   - cache/ is the transcode cache, disposable by design.
 *   - client/ is the window's own WebView2 profile, which lives in the per-user
 *     folder and is open while the app runs. It is not the library.
 *   - lancast.db.<anything> is an earlier set-aside database or a half-made
 *     copy. Carrying those along would make the destination look as though it
 *     had history it does not.
 *   - lancast.db-shm is the WAL index, which SQLite rebuilds from the WAL when
 *     it is missing. Copying a stale one is the only way to hurt a WAL copy.
 */
func skip(rel string) bool {
	top, _, _ := strings.Cut(rel, "/")
	switch {
	case top == "cache", top == "client":
		return true
	case top == DBName+"-shm":
		return true
	case strings.HasPrefix(top, DBName+"."):
		return true
	}
	return false
}

// destName is where a travelling file lands, relative to the destination.
// The database and its log arrive under the incoming name.
func destName(rel string) string {
	switch rel {
	case DBName:
		return incomingName
	case DBName + "-wal":
		return incomingName + "-wal"
	}
	return rel
}

type planEntry struct {
	rel  string
	size int64
	mod  time.Time
}

// copyPlan is everything that will travel, and how much of it there is.
type copyPlan struct {
	dirs  []string
	files []planEntry
	total int64
}

// planCopy walks a library folder. A link inside it is refused rather than
// followed or skipped: following one copies a drive somebody pointed it at,
// and skipping one silently leaves part of the library behind.
func planCopy(src string) (copyPlan, error) {
	var p copyPlan
	if _, err := os.Stat(filepath.Join(src, DBName)); err != nil {
		return p, fmt.Errorf("there is no library in %s: %w", src, err)
	}
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == src {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if skip(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		switch {
		case d.IsDir():
			p.dirs = append(p.dirs, rel)
		case d.Type().IsRegular():
			fi, err := d.Info()
			if err != nil {
				return err
			}
			p.files = append(p.files, planEntry{rel: rel, size: fi.Size(), mod: fi.ModTime()})
			p.total += fi.Size()
		default:
			return fmt.Errorf("%s is a link or a device, not a file; move it by hand and switch again", path)
		}
		return nil
	})
	return p, err
}

// copyLibrary copies a planned library into dst, the database under its
// incoming name. progress hears bytes copied so far against the total.
func copyLibrary(p copyPlan, src, dst string, progress func(done, total int64)) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, d := range p.dirs {
		if err := os.MkdirAll(filepath.Join(dst, filepath.FromSlash(d)), 0o755); err != nil {
			return err
		}
	}
	var done int64
	for _, f := range p.files {
		from := filepath.Join(src, filepath.FromSlash(f.rel))
		to := filepath.Join(dst, filepath.FromSlash(destName(f.rel)))
		n, err := copyFile(from, to, func(n int64) {
			if progress != nil {
				progress(done+n, p.total)
			}
		})
		if err != nil {
			return fmt.Errorf("copy %s: %w", from, err)
		}
		done += n
		// Kept, because the artwork cache and the optimized copies are judged
		// by age in places, and a move should not make everything new.
		_ = os.Chtimes(to, f.mod, f.mod)
	}
	if progress != nil {
		progress(done, p.total)
	}
	return nil
}

// copyFile copies one file, replacing whatever is at to. A new file inherits
// its folder's permissions, which is why the library is copied and not moved
// (ADR 0092 §3).
func copyFile(from, to string, progress func(int64)) (int64, error) {
	in, err := os.Open(from)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	out, err := os.Create(to)
	if err != nil {
		return 0, err
	}
	buf := make([]byte, 1<<20)
	var n int64
	for {
		r, rerr := in.Read(buf)
		if r > 0 {
			if _, werr := out.Write(buf[:r]); werr != nil {
				out.Close()
				return n, werr
			}
			n += int64(r)
			progress(n)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			out.Close()
			return n, rerr
		}
	}
	// Synced, because the next step trusts these bytes enough to set the
	// original aside.
	if err := out.Sync(); err != nil {
		out.Close()
		return n, err
	}
	return n, out.Close()
}

// clearIncoming removes a half-made copy's database files. They are the only
// thing this package ever deletes, and they are never a library: the name is
// the proof.
func clearIncoming(dir string) error {
	for _, name := range []string{incomingName, incomingName + "-wal", incomingName + "-shm"} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// stamp is the suffix a set-aside database carries.
func stamp(now time.Time) string { return now.Format("20060102-150405") }

/*
 * setAside renames a folder's database out of the way, with whichever of its
 * sidecars exist, and reports the new name ("" when there was nothing).
 *
 * A rename and never a delete (ADR 0092 §5). Every check that decides which
 * mode a machine is in looks for exactly lancast.db, so taking that name away
 * is the whole switch, and the file is still there to be put back by hand.
 *
 * The sidecars go too, and that is not tidiness: a lancast.db-wal left behind
 * would attach itself to the next lancast.db to arrive, which is a database
 * replayed with another database's log.
 */
func setAside(dir, label string, now time.Time) (string, error) {
	base := DBName + "." + label + "-" + stamp(now)
	moved := ""
	for _, suffix := range append([]string{""}, sidecars...) {
		from := filepath.Join(dir, DBName+suffix)
		if _, err := os.Stat(from); errors.Is(err, os.ErrNotExist) {
			continue
		}
		to := filepath.Join(dir, base+suffix)
		if err := os.Rename(from, to); err != nil {
			return moved, fmt.Errorf("set aside %s: %w", from, err)
		}
		if suffix == "" {
			moved = to
		}
	}
	return moved, nil
}

/*
 * prove decides whether a copy may become a library, and leaves it as one
 * file.
 *
 * verify runs first and decides everything: until it passes, the folder holds
 * only an incoming file and whatever it held before. Verify also folds the
 * copy's log into it, and a log still holding frames afterwards means it did
 * not — renaming the database without its log would lose them — so that is
 * refused too.
 */
func prove(dir string, verify func(string) error) error {
	incoming := filepath.Join(dir, incomingName)
	if err := verify(incoming); err != nil {
		return fmt.Errorf("the copy did not open, so nothing was changed: %w", err)
	}
	if fi, err := os.Stat(incoming + "-wal"); err == nil && fi.Size() > 0 {
		return fmt.Errorf("the copy's log was not folded in (%d bytes remain)", fi.Size())
	}
	for _, s := range sidecars {
		if err := os.Remove(incoming + s); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

/*
 * place makes a proven copy the folder's library.
 *
 * A database already here is set aside first — it belongs to the mode that is
 * not running — and the copy takes the name. The rename is the only moment
 * the folder changes meaning, and on one volume it is atomic.
 *
 * Returns the name a superseded database was given, "" when there was none.
 */
func place(dir string, now time.Time) (string, error) {
	superseded, err := setAside(dir, "superseded", now)
	if err != nil {
		return superseded, err
	}
	if err := os.Rename(filepath.Join(dir, incomingName), filepath.Join(dir, DBName)); err != nil {
		return superseded, err
	}
	return superseded, nil
}

// HasLibrary reports whether a folder holds a library.
func HasLibrary(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, DBName))
	return err == nil
}
