//go:build !windows

package service

// RunningServer has no implementation off Windows yet.
//
// The equivalent exists — asking systemd whether lancastd.service is active and
// reading its ExecStart — but the guard elsewhere is an advisory file lock whose
// holder is not the service in the same one-installed-service sense, and
// inventing a half-answer for a platform nobody has hit this on would be worse
// than the honest nothing. The caller already handles the unknown case: it
// prints the generic message it printed before.
func RunningServer() (Running, bool) { return Running{}, false }
