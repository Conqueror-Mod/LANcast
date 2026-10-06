//go:build !windows

package main

import "errors"

// startGalaxy exists only on Windows, where GOG Galaxy does.
func startGalaxy(string, []string) error {
	return errors.New("GOG Galaxy runs only on Windows")
}
