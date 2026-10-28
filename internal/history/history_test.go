package history

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// The ring holds 48 hours and no more, oldest first.
func TestTheRingKeepsTwoDays(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	var r Ring
	for i := 72 * 60; i >= 0; i -= 60 { // three days, a sample an hour
		r.Add(Sample{At: now.Add(-time.Duration(i) * time.Minute).Unix(), Watching: i})
	}
	all := r.Since(time.Unix(0, 0))
	if len(all) != 49 || all[0].At != now.Add(-48*time.Hour).Unix() || all[48].At != now.Unix() {
		t.Fatalf("%d samples from %d to %d", len(all), all[0].At, all[len(all)-1].At)
	}
	if got := r.Since(now.Add(-6 * time.Hour)); len(got) != 6 {
		t.Fatalf("the last six hours: %d samples", len(got))
	}
}

// Saved and loaded across a restart, keeping what is still inside the window
// and nothing from a clock that has since gone back.
func TestHistorySurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	now := time.Unix(1_800_000_000, 0)
	var r Ring
	r.Add(Sample{At: now.Add(-49 * time.Hour).Unix()})
	r.Add(Sample{At: now.Add(-time.Hour).Unix(), Watching: 2, CPU: 41.5})
	r.Add(Sample{At: now.Add(time.Hour).Unix()}) // from the future
	if err := r.Save(path); err != nil {
		t.Fatal(err)
	}
	got := Load(path, now).Since(time.Unix(0, 0))
	if len(got) != 1 || got[0].Watching != 2 || got[0].CPU != 41.5 {
		t.Fatalf("loaded %+v", got)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("the temporary file was left behind")
	}
}

// A missing or broken file is an empty history, not a reason to stop.
func TestABrokenHistoryFileIsEmpty(t *testing.T) {
	dir := t.TempDir()
	if got := Load(filepath.Join(dir, "none.json"), time.Now()).Since(time.Unix(0, 0)); len(got) != 0 {
		t.Fatal(got)
	}
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte("{not json"), 0o644)
	var r Ring
	r.Add(Sample{At: time.Now().Unix()})
	r.LoadInto(bad, time.Now())
	if got := r.Since(time.Unix(0, 0)); len(got) != 0 {
		t.Fatalf("a broken file loaded as %v", got)
	}
}

// The first reading has nothing to compare with; the second is a share of
// the machine, where the platform can be read at all.
func TestCPUIsAShareOfTheMachine(t *testing.T) {
	var c CPU
	if got := c.Percent(); got != -1 {
		t.Fatalf("first reading %v, want -1", got)
	}
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("no CPU reading on " + runtime.GOOS)
	}
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) { // some work, so the clock moves
	}
	got := c.Percent()
	if got < 0 || got > 100 {
		t.Fatalf("second reading %v", got)
	}
}
