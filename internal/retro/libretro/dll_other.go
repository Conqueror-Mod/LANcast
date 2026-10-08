//go:build !windows

package libretro

import "errors"

// Open is Windows only: the desktop client that hosts cores is a Windows
// program (ADR 0023), and a binding nothing on another platform can call is
// one nothing would test.
func Open(path string) (Core, error) {
	return nil, errors.New("libretro: cores are hosted on Windows only")
}
