//go:build !windows

package main

import (
	"fmt"
	"os"
)

// bareLaunchUsesTray: elsewhere a bare launch runs the server foreground, which
// is the expected behaviour in a terminal and under systemd.
const bareLaunchUsesTray = false

// alert falls back to stderr where a console exists.
func alert(title, msg string) {
	fmt.Fprintf(os.Stderr, "%s: %s\n", title, msg)
}
