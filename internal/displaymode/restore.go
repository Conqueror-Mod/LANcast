package displaymode

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

/*
 * The restore record: what to undo if this process dies with a display
 * switched.
 *
 * A switch is made with CDS_FULLSCREEN, which Windows calls temporary and
 * never writes to the registry. Whether Windows itself undoes it when the
 * process that made it dies is not something this file assumes: it is written
 * *before* the switch and removed *after* the restore, so a record still on
 * disk at startup means the last run did not restore, whatever the reason.
 * Recovering is then one call that puts the monitor back to its registry mode,
 * which is the mode the person chose, since nothing here ever writes one.
 *
 * The record names the monitor by its GDI device name (\\.\DISPLAY3). That
 * name is renumbered when the driver re-enumerates (displayconfig_windows.go
 * in clientwindow tells that story), which would make a stale record point at
 * the wrong screen or none. The cost is bounded: resetting a monitor to its
 * registry mode is what Windows does at every boot anyway.
 */

// Record is one switch that has not been undone yet.
type Record struct {
	Device string `json:"device"`
	FromHz int    `json:"from_hz"`
	ToHz   int    `json:"to_hz"`
}

// recordFile is the record's name in the client's data directory.
const recordFile = "display-restore.json"

// WriteRecord saves r before the switch it describes is made.
func WriteRecord(dir string, r Record) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, recordFile), b, 0o600)
}

// ReadRecord returns the record left on disk, if there is one. An unreadable
// record is reported as absent and removed: nothing could act on it anyway,
// and leaving it would retry the same failure at every launch.
func ReadRecord(dir string) (Record, bool) {
	p := filepath.Join(dir, recordFile)
	b, err := os.ReadFile(p)
	if err != nil {
		return Record{}, false
	}
	var r Record
	if json.Unmarshal(b, &r) != nil || r.Device == "" {
		_ = os.Remove(p)
		return Record{}, false
	}
	return r, true
}

// ClearRecord removes the record once the switch is undone. A record that is
// already gone is not an error.
func ClearRecord(dir string) error {
	err := os.Remove(filepath.Join(dir, recordFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
