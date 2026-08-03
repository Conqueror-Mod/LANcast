//go:build !devseed

package main

import "errors"

// runDevSeed is absent from ordinary builds.
//
// The seed creates libraries pointing at arbitrary paths and exists only to
// make development repeatable. Gating it behind a build tag means release
// binaries do not merely hide it — they do not contain it, which is a stronger
// statement than a command that is undocumented but present.
//
// Build with it when you need it:
//
//	go build -tags devseed -o LANcast-Server.exe ./cmd/lancastd
func runDevSeed([]string) error {
	return errors.New("this build has no devseed command; rebuild with -tags devseed")
}
