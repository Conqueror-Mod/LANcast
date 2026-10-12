//go:build slowscan

package scan

import "time"

/*
 * slowWalk makes every scan wait 20 ms per file, for testing the tray's
 * Cancel against a real library on the installed service.
 *
 * An unchanged library scans in seconds, faster than anybody can open a menu,
 * so a scan has to be made slow to be cancelled by hand. Behind a build tag
 * like devseed, so no release can contain it: `go build -tags slowscan`.
 * It changes nothing a scan writes, only how long it takes to look.
 */
var slowWalk = func(int64) { time.Sleep(20 * time.Millisecond) }
