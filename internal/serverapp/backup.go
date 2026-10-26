package serverapp

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"lancast/internal/api"
	"lancast/internal/backup"
	"lancast/internal/config"
	"lancast/internal/store"
	"lancast/internal/tasks"
)

/*
 * The backup schedule (docs/settings-plan.md, step 4).
 *
 * The same shape as the prune timer: wait for startup to settle, then check
 * on a short tick whether a backup is due, re-reading the interval each time
 * so a change in Settings takes effect without a restart. Whether one is due
 * is read from the newest scheduled file rather than a stamp of its own, so a
 * restart, a restore, or somebody deleting the files by hand all leave the
 * schedule telling the truth.
 */

const (
	backupTick     = 30 * time.Minute
	backupSettleIn = 5 * time.Minute
)

// backupMu keeps the timer's run and a Run now from taking two at once. The
// API serialises the snapshot itself; this keeps the board honest about it.
var backupMu sync.Mutex

func backupEvery(settings *config.SettingsStore) time.Duration {
	return time.Duration(settings.Get().BackupEveryDays) * 24 * time.Hour
}

func periodicBackup(ctx context.Context, srv *api.Server, settings *config.SettingsStore, board *tasks.Board) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(backupSettleIn):
	}
	t := time.NewTicker(backupTick)
	defer t.Stop()
	for {
		if backup.Due(srv.LastScheduledBackup(), time.Now(), backupEvery(settings)) {
			trackedBackup(ctx, srv, board)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// trackedBackup is one scheduled backup, reported to the board.
func trackedBackup(ctx context.Context, srv *api.Server, board *tasks.Board) {
	backupMu.Lock()
	defer backupMu.Unlock()
	_ = board.Track("backup", func() error { return srv.RunScheduledBackup(ctx) })
}

/*
 * preMigrationBackup is the hook store.OpenWith calls before an update changes
 * the database: a backup at the old revision, in the ordinary backups folder,
 * so Settings lists it and Restore can use it like any other.
 *
 * A failure is logged and does not stop the server. Refusing to start would
 * leave the service down after an update with nobody at the machine, for a
 * migration that has run without a safety copy on every update until now.
 */
func preMigrationBackup(dataDir string, log *slog.Logger) func(int, int, func(string) (store.Snapshot, error)) error {
	return func(from, to int, snapshot func(string) (store.Snapshot, error)) error {
		start := time.Now()
		f, removed, err := backup.New(dataDir).PreMigrate(start, from, snapshot)
		if err != nil {
			log.Error("backup before the database update failed; updating without one",
				"from", from, "to", to, "error", err)
			return nil
		}
		log.Info("backup taken before the database update",
			"name", f.Name, "from", from, "to", to, "bytes", f.Bytes,
			"took", time.Since(start).Round(time.Millisecond))
		for _, name := range removed {
			log.Info("older pre-update backup rotated out", "name", name)
		}
		return nil
	}
}
