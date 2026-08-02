//go:build !windows

package main

import (
	"fmt"
	"os"
)

// alert falls back to stderr where a console exists.
func alert(title, msg string) {
	fmt.Fprintf(os.Stderr, "%s: %s\n", title, msg)
}
