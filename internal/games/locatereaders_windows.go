package games

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

/*
 * Where Epic and Battle.net keep what this package reads.
 *
 * Beside steamRoot for the same reason it is split out: locating is the only
 * OS-specific part, and everything after it is a directory or a slice away from
 * being testable anywhere.
 */

/*
 * epicManifestDir is where the Epic launcher writes one `.item` per install.
 *
 * A fixed path under ProgramData rather than something read from the registry,
 * because that is where the launcher puts it and there is no key naming it. The
 * registry does record where the launcher itself is installed, which is a
 * different fact and not the one this needs: the manifests do not move when the
 * launcher does, and games do not live there at all — a manifest's
 * InstallLocation can point anywhere, which is the whole point of a games
 * library on another drive.
 *
 * ProgramData is resolved from the environment rather than hard-coded to
 * `C:\ProgramData`; it is relocatable, and a machine that has moved it is
 * exactly the machine a hard-coded path fails on silently.
 */
func epicManifestDir() (string, bool) {
	base := os.Getenv("ProgramData")
	if base == "" {
		base = `C:\ProgramData`
	}
	dir := filepath.Join(base, "Epic", "EpicGamesLauncher", "Data", "Manifests")
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		return dir, true
	}
	return "", false
}

/*
 * installedPrograms reads the uninstall registry.
 *
 * Three roots, and all three are needed: a 64-bit program registers under
 * SOFTWARE, a 32-bit one under WOW6432Node — Battle.net games are 32-bit
 * installers, so that is where Hearthstone is — and per-user installs land in
 * HKCU rather than HKLM.
 *
 * Errors on a single key are skipped rather than returned. This walks a few
 * hundred entries written by every installer that has ever run on the machine;
 * one of them being malformed is ordinary, and must not empty the games grid.
 */
func installedPrograms() []InstalledProgram {
	roots := []struct {
		key  registry.Key
		path string
	}{
		{registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`},
	}

	var out []InstalledProgram
	seen := map[string]bool{}
	for _, r := range roots {
		root, err := registry.OpenKey(r.key, r.path, registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			continue
		}
		names, err := root.ReadSubKeyNames(-1)
		root.Close()
		if err != nil {
			continue
		}
		for _, name := range names {
			k, err := registry.OpenKey(r.key, r.path+`\`+name, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			p := InstalledProgram{Key: name}
			p.Name, _, _ = k.GetStringValue("DisplayName")
			p.InstallLocation, _, _ = k.GetStringValue("InstallLocation")
			p.Publisher, _, _ = k.GetStringValue("Publisher")
			if size, _, err := k.GetIntegerValue("EstimatedSize"); err == nil {
				p.EstimatedSizeKB = int64(size)
			}
			k.Close()

			// The same program can appear under more than one root. Keyed on
			// the subkey name, which is what identifies an entry.
			if p.Name == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, p)
		}
	}
	return out
}
