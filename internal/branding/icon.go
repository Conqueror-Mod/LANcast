// Package branding holds shared brand assets compiled into the executables — the
// icon that shows in the Windows tray and (via a resource) in Explorer, so a
// built exe carries the LANcast identity with no external file (ADR 0022).
package branding

import _ "embed"

// IconICO is the multi-size LANcast icon, for the system tray. The same source
// image drives the exe's Explorer icon through the committed *_windows.syso.
//
//go:embed lancast.ico
var IconICO []byte
