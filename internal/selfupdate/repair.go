package selfupdate

import (
	"os"
	"path/filepath"
	"sort"
)

/*
 * RepairPermissions gives every program file in installDir its folder's
 * permissions again, and reports the ones that had something else.
 *
 * Apply now does this as it moves each file in. This is for the files an
 * earlier Apply moved without it: the update is applied by the version being
 * left, on its way down, so an install updating away from 0.11.1 has its new
 * files locked by 0.11.1's Apply whatever the new version does. Only the new
 * version, starting as the service, can put them right, and it must, or the
 * window and the tray cannot start: they run LANcast.exe as the person signed
 * in, and a file only SYSTEM and Administrators can read is one they cannot.
 *
 * Restricted to InstallFiles, the names an update can write, so a repair never
 * touches anything an installer or a person put there. A missing file is
 * skipped; an error on one file does not stop the rest.
 */
func RepairPermissions(installDir string) (repaired []string, errs []error) {
	names := make([]string, 0, len(InstallFiles))
	for name := range InstallFiles {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		p := filepath.Join(installDir, name)
		if _, err := os.Stat(p); err != nil {
			continue
		}
		before, err := dacl(p)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if err := inheritFromParent(p); err != nil {
			errs = append(errs, err)
			continue
		}
		if after, err := dacl(p); err == nil && after != before {
			repaired = append(repaired, name)
		}
	}
	return repaired, errs
}
