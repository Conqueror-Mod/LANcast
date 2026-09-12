package games

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

/*
 * Where Steam is.
 *
 * HKCU\Software\Valve\Steam\SteamPath is what the running client writes about
 * itself, and it is the per-user fact: this package is asked by the desktop
 * client on behalf of the person sitting in front of it, and that is the person
 * whose Steam matters.
 *
 * HKLM's InstallPath is the fallback, for the case where Steam is installed but
 * has not yet run for this user. It is under WOW6432Node because Steam is a
 * 32-bit program on a 64-bit Windows.
 *
 * Both are checked for actually being a directory before being believed. An
 * uninstall can leave the key behind, and a path from the registry that is not
 * there is "not installed", not an error worth showing anybody.
 */
func steamRoot() (string, bool) {
	for _, c := range []struct {
		key  registry.Key
		path string
		name string
	}{
		{registry.CURRENT_USER, `Software\Valve\Steam`, "SteamPath"},
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Valve\Steam`, "InstallPath"},
		{registry.LOCAL_MACHINE, `SOFTWARE\Valve\Steam`, "InstallPath"},
	} {
		k, err := registry.OpenKey(c.key, c.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		v, _, err := k.GetStringValue(c.name)
		k.Close()
		if err != nil || v == "" {
			continue
		}
		// SteamPath is written with forward slashes ("d:/games/steam"), which
		// Windows accepts everywhere but which would make every path this
		// package returns look wrong in the UI.
		dir := filepath.Clean(filepath.FromSlash(v))
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			return dir, true
		}
	}
	return "", false
}
