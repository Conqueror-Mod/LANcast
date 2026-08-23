//go:build devseed

package main

import "os"

/*
 * A second server on one machine, for federation work only.
 *
 * `internal/singleton` allows one server per machine, which is right: two
 * instances racing for a port and a database is a failure that only reproduces
 * where it happens. But federation is two servers by definition, and until this
 * existed there was no way to exercise pairing, presence or a remote guest
 * without a second physical machine — so the phase that most needs testing was
 * the one that could not be tested at all.
 *
 * `LANCAST_INSTANCE` names this instance. A non-empty value joins the lock
 * name, so `LANCAST_INSTANCE=georgia` takes `LANcast-Server-georgia` and does
 * not contend with an ordinary server. It changes nothing else: the data
 * directory and the listen address are already flags, and both must be given
 * different values too, because the lock is what stops two servers colliding
 * and not what keeps them apart.
 *
 *	go build -tags devseed -o dev-server.exe ./cmd/lancastd
 *	LANCAST_INSTANCE=georgia ./dev-server.exe -data <dir> -addr 127.0.0.1:8081
 *
 * It is an environment variable rather than a flag on purpose: a flag would
 * appear in `-help` on any build carrying this file and read as a supported way
 * to run several servers, which it is not. It is a testing affordance, and it
 * is **absent from release binaries** rather than merely undocumented in them —
 * the same standing devseed itself has, for the same reason.
 */
func instanceSuffix() string {
	if name := os.Getenv("LANCAST_INSTANCE"); name != "" {
		return "-" + name
	}
	return ""
}
