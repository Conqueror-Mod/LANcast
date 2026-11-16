// Package dataperm gives the service's data directory permissions of its own
// (R10-2, R10-3, R10-5, R12-1).
//
// As a Windows service the data directory is C:\ProgramData\LANcast, and
// before this package it kept ProgramData's defaults: every local account
// could read every file in it — the server's identity and TLS keys,
// secrets.key, the database, the backups — and create files and folders in
// any folder of it. Creating was the worse half: a planted update, a planted
// restore or a planted face worker each became a way to SYSTEM, or to being a
// LANcast administrator, that skipped UAC.
//
// The directory now has Administrators as its owner and a protected DACL:
// SYSTEM and Administrators have full control of everything, inherited, and
// Users may list the top folder's names and nothing else. Two reads by the
// unelevated desktop app need more than nothing, and are all it is given:
//
//   - whether lancast.db exists, which is how the app knows the machine-wide
//     library is the service's (cmd/lancast sharedDataDir, servermode
//     HasLibrary). Listing the top folder is what lets that stat succeed;
//     NTFS grants read-attributes on an entry to whoever may list its folder.
//   - tls\cert.pem, the public certificate the window pins (certpin.SPKI).
//     ShareCert grants Users read on that one file. key.pem beside it gets
//     nothing.
//
// It is applied by `service install`, by a switch to the service before the
// library is copied in, and on every start of the service. A start that finds
// the folder already so leaves it alone, because applying it walks the whole
// tree: tens of thousands of artwork and preview files on a real library.
package dataperm

import "log/slog"

// Report is what one Protect did, for the log.
type Report struct {
	// Applied is true when the folder's owner or DACL was not already right
	// and was rewritten, which also re-derives every inherited entry inside.
	Applied bool
	// Reclaimed counts entries inside that the #1096 guard refused — owned by
	// someone other than SYSTEM or Administrators, or writable by someone else
	// — and which were given to Administrators with only inherited
	// permissions. Each is something an unprivileged account made, or opened
	// up, while the folder was open to it.
	Reclaimed int
	// Links are links or junctions inside that the guard refused. They are
	// reported, not removed: deleting a path is not this package's call.
	Links []string
}

// LogReport writes r in the words an operator reading lancastd.log needs.
func LogReport(log *slog.Logger, dir string, r Report, err error) {
	if err != nil {
		log.Error("could not protect the data directory; any local account may be able to read or add files in it",
			"dir", dir, "error", err,
			"hint", "run `LANcast.exe service install` from an elevated shell, or reinstall")
		return
	}
	if r.Applied {
		log.Info(protectedSummary,
			"dir", dir, "reclaimed", r.Reclaimed)
	}
	for _, l := range r.Links {
		log.Warn("a link inside the data directory was made by an account that is not an administrator",
			"path", l, "hint", "remove it unless you made it yourself")
	}
}
