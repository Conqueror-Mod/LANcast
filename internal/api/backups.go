package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"lancast/internal/backup"
	"lancast/internal/store"
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
	// RestoreCommand is what to type to put one back by hand, with the
	// server stopped. Still shown beside the Restore button, because it is
	// the way back when the server will not start at all.
	RestoreCommand string `json:"restore_command"`
	// CanRestore is whether this server can restart itself to apply a
	// restore (POST /api/backups/{name}/restore). False where nothing would
	// start it again, and then only the command works.
	CanRestore bool `json:"can_restore"`
	// Staged is a restore waiting for the next start, or null.
	Staged *backup.Pending `json:"staged"`
	// LastRestore is what the most recent restore from Settings did, or null.
	LastRestore *backup.Outcome `json:"last_restore"`
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
		CanRestore:     s.canRestartSelf(),
		Staged:         dir.Staged(),
		LastRestore:    dir.LastRestore(),
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
	f, err := s.takeBackup(r.Context(), false)
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
	s.backupTaken(f.Name, f.Bytes, f.SchemaVersion, false)
	s.audit(r, "backup.create", "backup", f.Name,
		fmt.Sprintf("Took a backup (%s)", f.Name),
		map[string]any{"bytes": f.Bytes, "schema_version": f.SchemaVersion})
	writeJSON(w, http.StatusCreated, f)
}

// takeBackup writes one, by hand or for the schedule; the name says which.
func (s *Server) takeBackup(ctx context.Context, scheduled bool) (backup.File, error) {
	// One at a time. Two backups started together would race for a name, and
	// the second would fail on a collision that is this server's fault rather
	// than the caller's — a double-clicked button should take one backup, not
	// produce an error, and nor should a button pressed while the schedule runs.
	s.backupMu.Lock()
	defer s.backupMu.Unlock()

	dir := s.backupDir()
	if err := dir.Create(); err != nil {
		return backup.File{}, err
	}
	namer := backup.NewName
	if scheduled {
		namer = backup.NewScheduledName
	}
	path, name, err := s.freeBackupName(dir, namer)
	if err != nil {
		return backup.File{}, fmt.Errorf("name backup: %w", err)
	}
	snap, err := s.st.Snapshot(ctx, path)
	if err != nil {
		return backup.File{}, err
	}
	return backup.File{
		Name:          name,
		Bytes:         snap.Bytes,
		TakenAt:       snap.TakenAt,
		SchemaVersion: snap.SchemaVersion,
		Restorable:    true,
		Scheduled:     scheduled,
	}, nil
}

/*
 * RunScheduledBackup is one turn of the backup schedule: take a backup, then
 * delete the scheduled ones past the number to keep.
 *
 * Not audited. The audit trail is who did what, and nobody did this; the log
 * line is where it belongs, next to whatever else the server was doing. The
 * deletions are logged by name for the same reason — a backup that vanished
 * should be findable as "rotated out", not as a mystery.
 *
 * Rotation runs only after a backup has been written. A schedule failing on a
 * full disk must not then delete the copies it already has.
 */
func (s *Server) RunScheduledBackup(ctx context.Context) error {
	f, err := s.takeBackup(ctx, true)
	if err != nil {
		s.log.Error("scheduled backup failed", "error", err)
		return err
	}
	s.log.Info("scheduled backup taken", "name", f.Name, "bytes", f.Bytes)
	s.backupTaken(f.Name, f.Bytes, f.SchemaVersion, true)

	dir := s.backupDir()
	files, err := dir.List()
	if err != nil {
		return fmt.Errorf("list backups: %w", err)
	}
	for _, name := range backup.Expired(files, s.settings.Get().BackupKeep) {
		if err := dir.Remove(name); err != nil {
			s.log.Warn("scheduled backup: remove old one", "name", name, "error", err)
			continue
		}
		s.log.Info("scheduled backup rotated out", "name", name)
	}
	// An open Backup pane has no other way to hear about a backup nobody
	// pressed a button for.
	s.Publish(TopicBackups)
	return nil
}

// LastScheduledBackup is when the schedule last took one, for the task board.
func (s *Server) LastScheduledBackup() time.Time {
	files, err := s.backupDir().List()
	if err != nil {
		return time.Time{}
	}
	return backup.LastScheduled(files)
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
func (s *Server) freeBackupName(dir *backup.Dir, namer func(time.Time) string) (path, name string, err error) {
	now := time.Now()
	for n := 0; n < 60; n++ {
		name = namer(now.Add(time.Duration(n) * time.Second))
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
		return `LANcast.exe restore -from <file> -yes`
	}
	return "./lancast restore -from <file> -yes"
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

/*
 * restoreBackup puts a backup back, from Settings. Admin only.
 *
 * It stages the restore and restarts the server; the next start applies it
 * before it opens the database (backup.ApplyPending), because a server cannot
 * swap out the database it is holding open. Answers 202 once the restart is
 * under way.
 *
 * Before staging, a backup of the database as it is now is taken, by hand, so
 * it is never rotated away. Restoring the wrong backup is then undone from the
 * same list, rather than from the `.replaced-…` file the restore also leaves
 * beside the database, which only somebody at the machine can reach.
 *
 * Everyone is signed out by it. The sessions belong to the database being
 * replaced, and a backup holds none (ADR 0058).
 */
func (s *Server) restoreBackup(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	dir := s.backupDir()
	path, err := dir.Resolve(name)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such backup")
		return
	}
	if _, err := os.Stat(path); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such backup")
		return
	}
	if _, err := store.InspectSnapshot(path); err != nil {
		var tooNew *store.SnapshotTooNewError
		if errors.As(err, &tooNew) {
			writeError(w, http.StatusConflict, "not_restorable",
				"this backup was taken by a newer LANcast — update before restoring it")
			return
		}
		writeError(w, http.StatusConflict, "not_restorable", "this file is not a readable LANcast backup")
		return
	}
	if !s.canRestartSelf() {
		writeError(w, http.StatusPreconditionFailed, "cannot_restart",
			"this server cannot restart itself here — stop it and use the restore command instead")
		return
	}
	if dir.Staged() != nil {
		writeError(w, http.StatusConflict, "already_staged",
			"a restore is already waiting for the server to restart")
		return
	}

	safety, err := s.takeBackup(r.Context(), false)
	if err != nil {
		if isNoSpace(err) {
			writeError(w, http.StatusInsufficientStorage, "no_space",
				"not enough free space to back up the current database first, so nothing was restored")
			return
		}
		s.writeInternal(w, err, "back up before restoring")
		return
	}
	pending := backup.Pending{
		Name:         name,
		RequestedBy:  s.actorName(r),
		RequestedAt:  time.Now().Unix(),
		SafetyBackup: safety.Name,
	}
	if err := dir.Stage(pending); err != nil {
		s.writeInternal(w, err, "stage restore")
		return
	}
	// Audited before the restart, into the database being replaced: that is
	// the record of the decision, and it travels in the safety backup too.
	s.audit(r, "backup.restore", "backup", name,
		fmt.Sprintf("Restored the backup %s (the database before it was kept as %s)", name, safety.Name),
		map[string]any{"safety_backup": safety.Name})
	if err := s.restartSelf(); err != nil {
		// Nothing will apply it, so it must not wait for some later restart
		// nobody connects with this request.
		dir.Unstage()
		s.writeInternal(w, err, "restart to restore")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"staged":        pending,
		"safety_backup": safety,
	})
}
