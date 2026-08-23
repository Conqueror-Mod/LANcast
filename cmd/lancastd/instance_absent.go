//go:build !devseed

package main

// instanceSuffix is empty in every build that ships.
//
// One server per machine is the rule (`internal/singleton`), and it is a real
// rule rather than a convenience: two servers racing for the same port and the
// same database is a class of bug that only ever reproduces on the machine it
// happens on. Release binaries therefore have no way to weaken it — the
// suffix is a constant empty string and there is no flag, no environment
// variable and no code path that could produce anything else.
//
// The devseed build has a version that can, and says why.
func instanceSuffix() string { return "" }
