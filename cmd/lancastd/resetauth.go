package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"lancast/internal/config"
	"lancast/internal/desktop"
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
	if users == 0 {
		fmt.Println("no accounts exist; nothing to reset")
		fmt.Println("start LANcast and it will ask you to create one")
		return nil
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
