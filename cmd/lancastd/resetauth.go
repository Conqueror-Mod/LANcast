package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"lancast/internal/config"
	"lancast/internal/desktop"
	"lancast/internal/service"
	"lancast/internal/store"
)

// runResetAuth clears every account so a locked-out operator can create a fresh
// first admin.
//
// Deliberately a local subcommand rather than an HTTP endpoint. Someone who can
// authenticate does not need it, and someone who cannot must never be handed
// it — so the authority to use this is "can run a program on the server", which
// is the same authority that could read the database file directly anyway.
//
// Watch history survives: see store.DeleteAllUsers. Libraries, items, artwork
// and settings are untouched.
func runResetAuth(args []string) error {
	fs := flag.NewFlagSet("reset-auth", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory (default: the one the server uses)")
	addr := fs.String("addr", ":8080", "address the server listens on, used only to check it is stopped")
	yes := fs.Bool("yes", false, "actually do it; without this the command only reports what it would remove")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), `usage: lancastd reset-auth [-data DIR] [-yes]

Removes every LANcast account and session so you can create a new first admin.
Watch history, libraries, and settings are kept.

Stop the server first — it holds the database open.

Without -yes this reports what it would remove and changes nothing.
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Resolve(*addr, *dataDir)
	if err != nil {
		return err
	}

	// A running server holds the database. SQLite would surface that as a lock
	// timeout or a readonly error partway through, which reads as corruption
	// rather than "stop the service first", so check up front and say it plainly.
	if desktop.ServerRunning(*addr) {
		return errors.New("a LANcast server is still running and holds the database\n" +
			"stop it first, then run this again:\n" +
			"  Stop-Service lancastd     (installed as a service)\n" +
			"  or quit LANcast from the system tray")
	}

	st, err := store.Open(cfg.DBPath())
	if err != nil {
		return describeOpenFailure(cfg.DBPath(), err)
	}
	defer st.Close()

	ctx := context.Background()
	users, err := st.CountUsers(ctx)
	if err != nil {
		return err
	}
	sessions, err := st.CountSessions(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("database: %s\n", cfg.DBPath())
	others := otherDatabases(cfg.DataDir)

	if users == 0 {
		fmt.Println("no accounts exist; nothing to reset")
		// The likeliest reason someone recovering from a lockout sees this is
		// that they reset the wrong database. A service runs as another account
		// and keeps its data elsewhere, so the per-user default this resolves to
		// is not the one their server uses — and "nothing to reset" reads as
		// "the command did not work" rather than "you are pointed at an empty
		// database".
		if len(others) > 0 {
			fmt.Println()
			fmt.Println("but another LANcast database exists:")
			for _, o := range others {
				fmt.Printf("  %s\n", filepath.Join(o, "lancast.db"))
			}
			fmt.Println("if your server uses one of those, re-run against it:")
			// Plain quotes, not %q: on Windows %q escapes the separators into
			// C:\\ProgramData\\LANcast, and a suggested command line that cannot
			// be pasted is worse than none.
			fmt.Printf("  %s reset-auth -data \"%s\"\n", exeName(), others[0])
			return nil
		}
		fmt.Println("start LANcast and it will ask you to create one")
		return nil
	}

	// Accounts were found, but there is more than one database on this machine
	// and the caller did not say which they meant. Resetting is destructive to
	// the wrong one just as easily as the right one.
	if len(others) > 0 && *dataDir == "" {
		fmt.Println()
		fmt.Println("note: this is the per-user database, and another exists:")
		for _, o := range others {
			fmt.Printf("  %s\n", filepath.Join(o, "lancast.db"))
		}
		fmt.Println("a server installed as a service uses its own data directory — pass -data to be sure")
		fmt.Println()
	}

	if !*yes {
		fmt.Printf("would remove %s and %s\n", plural(users, "account"), plural(sessions, "session"))
		fmt.Println("watch history, libraries and settings are kept")
		fmt.Println("re-run with -yes to do it")
		return nil
	}

	removedUsers, removedSessions, err := st.DeleteAllUsers(ctx)
	if err != nil {
		return describeOpenFailure(cfg.DBPath(), err)
	}

	fmt.Printf("removed %s and %s\n",
		plural(int(removedUsers), "account"), plural(int(removedSessions), "session"))
	fmt.Println("start LANcast and create a new account — it takes the same id as the old one,")
	fmt.Println("so existing watch history reconnects to it")
	return nil
}

// otherDatabases returns data directories other than inUse that hold a LANcast
// database.
//
// The trap this exists for: a server installed as a service runs as another
// account and keeps its data in a system location, while this command run by a
// human resolves to the per-user default. Recovering from a lockout is exactly
// when someone runs it, and pointing at the wrong database reports "no accounts
// exist" — which reads as the command being broken rather than as being aimed
// somewhere else entirely.
//
// Existence is all that is checked. Opening a candidate would apply the schema
// and migrations to a database this command was not asked to touch, and
// creating one where none existed would be worse than the confusion it set out
// to prevent.
func otherDatabases(inUse string) []string {
	return existingDatabases(inUse, []string{
		service.DefaultDataDir(runtime.GOOS),
		perUserDataDir(),
	})
}

// existingDatabases is otherDatabases' rule, separated from where the candidate
// list comes from so it is testable without depending on the machine's
// environment.
func existingDatabases(inUse string, candidates []string) []string {
	var out []string
	seen := map[string]bool{}

	for _, dir := range candidates {
		if dir == "" {
			continue
		}
		abs, err := filepath.Abs(dir)
		if err != nil || seen[strings.ToLower(abs)] || sameDir(abs, inUse) {
			continue
		}
		seen[strings.ToLower(abs)] = true
		// A zero-byte file is a half-created database, not one holding accounts,
		// and pointing someone at it would send them somewhere useless.
		if st, err := os.Stat(filepath.Join(abs, "lancast.db")); err == nil && st.Size() > 0 {
			out = append(out, abs)
		}
	}
	return out
}

// perUserDataDir is the default this command resolves to when -data is absent,
// reported without creating anything.
func perUserDataDir() string {
	d, err := config.DefaultDataDir()
	if err != nil {
		return ""
	}
	return d
}

// sameDir compares paths case-insensitively, since Windows paths differing only
// in case are the same directory and naming it as an alternative would be noise.
func sameDir(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// exeName is how to spell this program in a suggested command line.
func exeName() string {
	exe, err := os.Executable()
	if err != nil {
		return "lancastd"
	}
	return filepath.Base(exe)
}

// describeOpenFailure turns the two failures an operator actually hits into
// instructions. A bare "attempt to write a readonly database (8)" sends people
// looking for corruption when the answer is "run this as Administrator".
func describeOpenFailure(path string, err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "readonly database"), os.IsPermission(err),
		strings.Contains(msg, "access is denied"), strings.Contains(msg, "permission denied"):
		return fmt.Errorf("cannot write %s: %w\n"+
			"the data directory is owned by the service account, so this needs an\n"+
			"elevated shell — reopen PowerShell as Administrator and run it again", path, err)
	case strings.Contains(msg, "database is locked"), strings.Contains(msg, "busy"):
		return fmt.Errorf("the database is locked by another process: %w\n"+
			"stop the LANcast server and try again", err)
	default:
		return err
	}
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}
