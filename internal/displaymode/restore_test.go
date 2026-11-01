package displaymode

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecordRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if _, ok := ReadRecord(dir); ok {
		t.Fatal("a fresh directory has no record")
	}
	r := Record{Device: `\\.\DISPLAY3`, FromHz: 60, ToHz: 23}
	if err := WriteRecord(dir, r); err != nil {
		t.Fatal(err)
	}
	got, ok := ReadRecord(dir)
	if !ok || got != r {
		t.Fatalf("got %+v, %v", got, ok)
	}
	if err := ClearRecord(dir); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadRecord(dir); ok {
		t.Fatal("record survived being cleared")
	}
	// Clearing twice is how stop-then-exit looks; it must not fail.
	if err := ClearRecord(dir); err != nil {
		t.Fatalf("second clear: %v", err)
	}
}

func TestUnreadableRecordIsDropped(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, recordFile)
	for _, body := range []string{"not json", `{"device":""}`} {
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, ok := ReadRecord(dir); ok {
			t.Fatalf("%q read as a record", body)
		}
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%q was left on disk to fail again at the next launch", body)
		}
	}
}
