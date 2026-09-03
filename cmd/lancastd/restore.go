package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"lancast/internal/config"
	"lancast/internal/desktop"
	"lancast/internal/store"
)

/*
 * runRestore replaces the database with a backup (ADR 0058).
 *
 * Offline, and deliberately not an HTTP endpoint. Restoring means swapping the
 * database out from under whatever holds it, and a server that could perform
 * this on itself would be replacing the file it is reading — which is how a
 * restore becomes the incident it was supposed to prevent.
 *
 * It is also the same authority argument reset-auth makes: anyone who can run
 * a program on the server can already replace the database file by hand.
 */
func runRestore(args []string) error {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	from := fs.String("from", "", "backup file to restore (required)")
	dataDir := fs.String("data", "", "data directory (default: the one the server uses)")
	addr := fs.String("addr", ":8080", "address the server listens on, used only to check it is stopped")
	yes := fs.Bool("yes", false, "actually do it; without this the command only reports what it would do")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), `usage: lancastd restore -from BACKUP [-data DIR] [-yes]

Replaces the LANcast database with a backup taken from Settings.

Stop the server first — it holds the database open.

The database being replaced is kept beside it under a stamped name, so a
restore of the wrong backup can be undone. Artwork is not in a backup and is
re-fetched over the following hours. Everyone is signed out.

Without -yes this reports what it would do and changes nothing.
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *from == "" {
		fs.Usage()
		return errors.New("restore: -from is required")
	}

	cfg, err := config.Resolve(*addr, *dataDir)
	if err != nil {
		return err
	}

	// Checked before anything is read, because the alternative is a lock
	// timeout partway through a file swap, which reads as corruption rather
	// than as "stop the service first".
	if desktop.ServerRunning(*addr) {
		return errors.New("a LANcast server is still running and holds the database\n" +
			"stop it first, then run this again:\n" +
			"  Stop-Service lancastd     (installed as a service)\n" +
			"  or quit LANcast from the system tray")
	}

	// The schema gate, run before the confirmation rather than after it: being
	// told a backup cannot be restored is worth knowing at the point of asking,
	// not at the point of committing.
	snap, err := store.InspectSnapshot(*from)
	if err != nil {
		return describeSnapshotFailure(*from, err)
	}

	fmt.Printf("backup:   %s\n", snap.Path)
	fmt.Printf("          %s, schema version %d\n", humanBytes(snap.Bytes), snap.SchemaVersion)
	fmt.Printf("database: %s\n", cfg.DBPath())
	if live, err := os.Stat(cfg.DBPath()); err == nil {
		fmt.Printf("          %s, will be kept as %s.replaced-…\n",
			humanBytes(live.Size()), filepath.Base(cfg.DBPath()))
	} else {
		fmt.Println("          does not exist yet; nothing will be replaced")
	}

	// The same warning reset-auth gives, and for the same reason: a server
	// installed as a service keeps its data somewhere a human running this
	// command does not resolve to by default, and restoring the wrong database
	// is as easy as restoring the right one.
	if others := otherDatabases(cfg.DataDir); len(others) > 0 && *dataDir == "" {
		fmt.Println()
		fmt.Println("note: this is the per-user database, and another exists:")
		for _, o := range others {
			fmt.Printf("  %s\n", filepath.Join(o, "lancast.db"))
		}
		fmt.Println("a server installed as a service uses its own data directory — pass -data to be sure")
	}

	if !*yes {
		fmt.Println()
		fmt.Println("would replace the database with this backup")
		if snap.SchemaVersion < store.CurrentSchemaVersion {
			fmt.Printf("the backup is older than this build and would be migrated forward to version %d\n",
				store.CurrentSchemaVersion)
		}
		fmt.Println("everyone would be signed out, and artwork would be re-fetched over the following hours")
		fmt.Println("re-run with -yes to do it")
		return nil
	}

	res, err := store.RestoreSnapshot(context.Background(), cfg.DBPath(), snap.Path)
	if err != nil {
		return describeSnapshotFailure(*from, err)
	}

	fmt.Println()
	fmt.Printf("restored %s\n", res.DBPath)
	if res.ReplacedPath != "" {
		fmt.Printf("the database it replaced is kept at %s\n", res.ReplacedPath)
		fmt.Println("delete it once you are satisfied this is the right backup")
	}
	if res.SchemaVersionAfter != snap.SchemaVersion {
		fmt.Printf("migrated forward from schema version %d to %d\n",
			snap.SchemaVersion, res.SchemaVersionAfter)
	}
	fmt.Printf("signed out %s\n", plural(res.SessionsCleared, "session"))
	fmt.Println()
	fmt.Println("start LANcast and sign in again.")
	fmt.Println("posters and other artwork are not in a backup and will be re-fetched over the")
	fmt.Println("following hours. If the media now lives somewhere else on this machine, edit the")
	fmt.Println("library's location in Settings and scan — the scan reconciles files, and every")
	fmt.Println("correction, rating and watch position in the backup is kept.")
	return nil
}

// describeSnapshotFailure turns the failures an operator actually hits into
// instructions, the way describeOpenFailure does for reset-auth.
func describeSnapshotFailure(path string, err error) error {
	var tooNew *store.SnapshotTooNewError
	switch {
	case errors.As(err, &tooNew):
		return fmt.Errorf("%w\n"+
			"this backup was taken by a newer LANcast than the one installed.\n"+
			"update LANcast first, then restore it", err)
	case errors.Is(err, store.ErrNotSnapshot):
		return fmt.Errorf("%s is not a LANcast backup: %w\n"+
			"a backup is the file written by Settings, named lancast-backup-….db", path, err)
	default:
		return describeOpenFailure(path, err)
	}
}

// humanBytes prints a size somebody can judge a backup by. Backups are the one
// place a person compares two numbers and decides which file is the real one.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d bytes", n)
	}
	div, exp := int64(unit), 0
	for size := n / unit; size >= unit; size /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}
