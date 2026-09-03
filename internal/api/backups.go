package api

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"lancast/internal/backup"
)

/*
 * Backups (ADR 0058).
 *
 * Taking one is an endpoint. **Restoring one deliberately is not**, and that
 * asymmetry is the design rather than an omission: restoring means replacing
 * the database this process is reading, and a server that could do that to
 * itself would be swapping the file out from under its own open transactions.
 * It is `lancastd restore` on the machine, offline, and the client says so
 * rather than offering a button that cannot exist.
 *
 * Everything here is admin-only. A backup is a complete copy of the library
 * including every account row, so being able to fetch one is being able to
 * read the database.
 */

// backupsResponse is the list plus where the files are.
type backupsResponse struct {
	Backups []backup.File `json:"backups"`
	// Folder is shown so somebody can go and copy a backup somewhere that is
	// not this disk. A backup that only exists on the drive it protects
	// against is not really a backup, and naming the folder is the cheapest
	// way to say that without a lecture.
	Folder string `json:"folder"`
	// RestoreCommand is what to type to put one back. The client cannot
	// perform a restore and should not pretend otherwise, so it shows this.
	RestoreCommand string `json:"restore_command"`
}

func (s *Server) backupDir() *backup.Dir { return backup.New(s.dataDir) }

// listBackups reports what has been taken. Admin only.
func (s *Server) listBackups(w http.ResponseWriter, r *http.Request) {
	dir := s.backupDir()
	files, err := dir.List()
	if err != nil {
		s.writeInternal(w, err, "list backups")
		return
	}
	writeJSON(w, http.StatusOK, backupsResponse{
		Backups:        files,
		Folder:         dir.Path(),
		RestoreCommand: restoreCommand(),
	})
}

/*
 * createBackup takes one. Admin only.
 *
 * Synchronous, and deliberately not a background job with progress. It is
 * VACUUM INTO on a database measured at 100 MB and it finishes in under a
 * second — an activity entry, a poll, and a spinner would be more machinery
 * than the operation, and they would report on something already over by the
 * time the first poll landed.
 */
func (s *Server) createBackup(w http.ResponseWriter, r *http.Request) {
	// One at a time. Two backups started together would race for a name, and
	// the second would fail on a collision that is this server's fault rather
	// than the caller's — a double-clicked button should take one backup, not
	// produce an error.
	s.backupMu.Lock()
	defer s.backupMu.Unlock()

	dir := s.backupDir()
	if err := dir.Create(); err != nil {
		s.writeInternal(w, err, "create backup folder")
		return
	}

	path, name, err := s.freeBackupName(dir)
	if err != nil {
		s.writeInternal(w, err, "name backup")
		return
	}

	snap, err := s.st.Snapshot(r.Context(), path)
	if err != nil {
		// No space is the one failure worth its own answer: it is the likely
		// one on a home server, and "unexpected server error" would send
		// somebody looking for a bug in LANcast instead of at their disk.
		if errors.Is(err, os.ErrNotExist) || isNoSpace(err) {
			writeError(w, http.StatusInsufficientStorage, "no_space",
				"not enough free space to write a backup")
			return
		}
		s.writeInternal(w, err, "take backup")
		return
	}

	s.audit(r, "backup.create", "backup", name,
		fmt.Sprintf("Took a backup (%s)", name),
		map[string]any{"bytes": snap.Bytes, "schema_version": snap.SchemaVersion})

	writeJSON(w, http.StatusCreated, backup.File{
		Name:          name,
		Bytes:         snap.Bytes,
		TakenAt:       snap.TakenAt,
		SchemaVersion: snap.SchemaVersion,
		Restorable:    true,
	})
}

/*
 * freeBackupName finds a name nothing is using.
 *
 * The name carries a timestamp to the second, and a backup takes well under
 * one — so two in the same second is not exotic, it is what a person pressing
 * the button twice produces. Snapshot refuses to overwrite, which is right,
 * and this is what stops that refusal from reaching somebody who did nothing
 * wrong.
 */
func (s *Server) freeBackupName(dir *backup.Dir) (path, name string, err error) {
	now := time.Now()
	for n := 0; n < 60; n++ {
		name = backup.NewName(now.Add(time.Duration(n) * time.Second))
		path, err = dir.Resolve(name)
		if err != nil {
			return "", "", err
		}
		if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
			return path, name, nil
		}
	}
	return "", "", fmt.Errorf("no free backup name near %s", now.Format(time.RFC3339))
}

/*
 * downloadBackup sends one. Admin only.
 *
 * The point of the whole feature: a backup sitting in the data directory is on
 * the same disk as the thing it protects. Getting it *off* that disk is what
 * makes it a backup, and asking somebody to find a folder on a headless server
 * is how that does not happen.
 */
func (s *Server) downloadBackup(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	path, err := s.backupDir().Resolve(name)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "not a backup name")
		return
	}

	f, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such backup")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		s.writeInternal(w, err, "stat backup")
		return
	}

	// Every backup name is ASCII by construction, so the RFC 5987 dance the
	// media download does is not needed here.
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	// ServeContent, so a 100 MB file over a home network can be resumed rather
	// than restarted when a laptop lid closes halfway through.
	http.ServeContent(w, r, name, info.ModTime(), f)
}

// deleteBackup removes one. Admin only.
func (s *Server) deleteBackup(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	dir := s.backupDir()

	if err := dir.Remove(name); err != nil {
		if errors.Is(err, backup.ErrBadName) {
			writeError(w, http.StatusBadRequest, "bad_request", "not a backup name")
			return
		}
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "not_found", "no such backup")
			return
		}
		s.writeInternal(w, err, "delete backup")
		return
	}

	// Audited like every other deliberate, destructive administrative act
	// (ADR 0026). Deleting the copy of the library is exactly the sort of thing
	// somebody wants to be able to look up afterwards.
	s.audit(r, "backup.delete", "backup", name, fmt.Sprintf("Deleted a backup (%s)", name), nil)
	w.WriteHeader(http.StatusNoContent)
}

// restoreCommand is the line to type, spelled for the platform the server is
// on. Shown rather than performed — see the note at the top of this file.
func restoreCommand() string {
	if runtime.GOOS == "windows" {
		return `LANcast-Server.exe restore -from <file> -yes`
	}
	return "./LANcast-Server restore -from <file> -yes"
}

/*
 * isNoSpace recognises a full disk.
 *
 * By message rather than by errno, because the error arrives through SQLite's
 * own reporting rather than as a Go syscall error — VACUUM INTO fails inside
 * the driver, and what comes back is text. Matching text is not lovely; the
 * alternative is answering "unexpected server error" to the single most likely
 * failure a home server has, which sends somebody looking for a bug in LANcast
 * rather than at their disk.
 */
func isNoSpace(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, s := range []string{"no space left", "not enough space", "disk full", "database or disk is full"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}
