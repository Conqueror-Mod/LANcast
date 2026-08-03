//go:build devseed

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"lancast/internal/config"
	"lancast/internal/media"
	"lancast/internal/probe"
	"lancast/internal/scan"
	"lancast/internal/store"
)

// devLibrary is one library the seed creates, named by the folder it expects
// under the test root.
type devLibrary struct {
	name   string
	kind   string
	folder string
	// note explains why a library is skipped, for kinds the server cannot scan
	// yet. Empty means it is created.
	note string
}

var devLibraries = []devLibrary{
	{name: "Movies", kind: media.LibraryMovie, folder: "TEST MOVIE LIBRARY"},
	{name: "TV Shows", kind: media.LibraryShow, folder: "TEST SHOWS LIBRARY"},
	{name: "Music", kind: media.LibraryMusic, folder: "TEST MUSIC LIBRARY"},
	{name: "Pictures", kind: media.LibraryOther, folder: "TEST PICTURE LIBRARY",
		note: "photo libraries are not implemented — ADR 0024 defers them, and " +
			"the scanner would index nothing"},
}

// runDevSeed points a development instance at a set of test libraries.
//
// It exists because recreating libraries and settings by hand at the start of
// every session is the tedious part of testing, and doing it by hand is where
// mistakes like scanning the *live* media folder come from.
//
// It deliberately does not create accounts, set passwords, or write API keys.
// A credential compiled into the program is a credential that ships, and this
// project's security posture does not survive one — see the loopback rule in
// CLAUDE.md. Create the account once, by hand, in a data directory you keep;
// everything else here is repeatable.
//
// Behind the `devseed` build tag, so it is absent from release binaries rather
// than merely undocumented in them.
func runDevSeed(args []string) error {
	fs := flag.NewFlagSet("devseed", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory to seed (default: the per-user one)")
	root := fs.String("root", filepath.Join("..", "TEST LIBRARIES"),
		"folder holding the TEST * LIBRARY directories")
	doScan := fs.Bool("scan", false, "scan each library after creating it")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), `usage: lancastd devseed [-data DIR] [-root DIR] [-scan]

Creates the development libraries pointing at the test folders under ROOT, and
turns off NFO writing so a scan cannot touch files on disk.

Idempotent: a library already pointing at a folder is left alone.

Never creates an account. Create one by hand, once, in a data directory you
keep between sessions.
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	absRoot, err := filepath.Abs(*root)
	if err != nil {
		return fmt.Errorf("resolve root: %w", err)
	}
	if st, err := os.Stat(absRoot); err != nil || !st.IsDir() {
		return fmt.Errorf("test library root not found: %s\npass -root to point at the folder holding the TEST * LIBRARY directories", absRoot)
	}

	cfg, err := config.Resolve(":8080", *dataDir)
	if err != nil {
		return err
	}

	// A running server holds the database; the same check reset-auth makes, for
	// the same reason — a lock timeout partway through reads as corruption.
	db, err := store.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("open database (is the server running?): %w", err)
	}
	defer db.Close()

	fmt.Printf("data:  %s\n", cfg.DataDir)
	fmt.Printf("root:  %s\n\n", absRoot)

	if err := seedSettings(cfg); err != nil {
		return err
	}

	ctx := context.Background()
	existing, err := db.ListLibraries(ctx)
	if err != nil {
		return fmt.Errorf("list libraries: %w", err)
	}

	var created []store.Library
	for _, want := range devLibraries {
		path := filepath.Join(absRoot, want.folder)

		if want.note != "" {
			fmt.Printf("  skip    %-10s %s\n", want.name, want.note)
			continue
		}
		if _, err := os.Stat(path); err != nil {
			fmt.Printf("  missing %-10s %s\n", want.name, path)
			continue
		}
		if lib, found := libraryAt(existing, path); found {
			fmt.Printf("  exists  %-10s id=%d\n", want.name, lib.ID)
			if *doScan {
				created = append(created, lib)
			}
			continue
		}

		lib, err := db.CreateLibrary(ctx, want.name, want.kind, path)
		if err != nil {
			return fmt.Errorf("create %s: %w", want.name, err)
		}
		fmt.Printf("  create  %-10s id=%d  %s\n", want.name, lib.ID, path)
		created = append(created, *lib)
	}

	if *doScan && len(created) > 0 {
		fmt.Println()
		if err := scanAll(db, created); err != nil {
			return err
		}
	}

	fmt.Println("\nseeded. create an account in the browser if this instance has none.")
	return nil
}

// seedSettings turns off the things that write outside the data directory.
//
// WriteNFO especially: a scan with it on writes sidecars *next to the media*,
// which on a real library means modifying files nobody asked to modify. That
// happened once during testing and is the reason this is not left to whatever
// the data directory already had.
func seedSettings(cfg config.Config) error {
	settings, err := config.LoadSettings(cfg.DataDir)
	if err != nil {
		return err
	}
	cur := settings.Get()
	cur.WriteNFO = false
	// Enrichment is left alone: it is a no-op without a key, and whether to
	// spend API calls during a test run is a per-session choice, not a fixture.
	if err := settings.Set(cur); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}
	fmt.Println("  settings write_nfo=false")
	return nil
}

func libraryAt(libs []store.Library, path string) (store.Library, bool) {
	for _, l := range libs {
		if sameDir(l.Path, path) {
			return l, true
		}
	}
	return store.Library{}, false
}

func scanAll(db *store.Store, libs []store.Library) error {
	log := newLogger(false)
	sc := scan.New(db, log)
	// Same wiring the server does, so a seeded scan produces the same rows a
	// real one would — including music titles read from tags.
	sc.SetTagReader(probe.New())

	for _, lib := range libs {
		if _, err := sc.Start(lib); err != nil {
			return fmt.Errorf("scan %s: %w", lib.Name, err)
		}
		p := waitForScan(sc, lib)
		fmt.Printf("  scan    %-10s %d files, %d changed, %d skipped\n",
			lib.Name, p.FilesSeen, p.ItemsChanged, p.Skipped)
	}
	return nil
}

// waitForScan blocks until a library's scan stops running.
func waitForScan(sc *scan.Scanner, lib store.Library) scan.Progress {
	for {
		p := sc.Status(lib.ID)
		if p.State != scan.StateRunning {
			return p
		}
		time.Sleep(200 * time.Millisecond)
	}
}
