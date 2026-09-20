package knownserver

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	accepted := time.Date(2026, 9, 20, 11, 30, 0, 0, time.UTC)
	want := List{}.Accept(Server{
		Address: "192.168.1.66:8080", Name: "Chris", Pin: "abc=", Accepted: accepted,
	})

	if err := Save(dir, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(got.Servers) != 1 {
		t.Fatalf("%d servers, want 1", len(got.Servers))
	}
	s := got.Servers[0]
	if s.Address != "192.168.1.66:8080" || s.Name != "Chris" || s.Pin != "abc=" {
		t.Errorf("round trip changed the record: %+v", s)
	}
	if !s.Accepted.Equal(accepted) {
		t.Errorf("accepted = %v, want %v", s.Accepted, accepted)
	}
}

// First run. Not an error, and not a reason to refuse to open anything.
func TestLoadWithNoFileIsEmpty(t *testing.T) {
	got, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Servers) != 0 {
		t.Errorf("%d servers on a first run", len(got.Servers))
	}
}

/*
 * A record that will not parse fails towards asking, not towards trusting.
 *
 * The caller is told, because a trust store that silently emptied itself would
 * present a fingerprint prompt with no explanation -- and the one situation
 * where somebody must read a fingerprint prompt carefully is the situation
 * where they have been trained that it appears for no reason.
 */
func TestLoadWithAnUnreadableFileIsEmptyAndSaysSo(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := Load(dir)
	if err == nil {
		t.Error("an unreadable trust store was reported as fine")
	}
	if len(got.Servers) != 0 {
		t.Errorf("%d servers recovered from an unreadable file", len(got.Servers))
	}
	// And the consequence, stated: everything is askable again.
	if trust := got.Check("192.168.1.66:8080", "abc="); trust != TrustUnknown {
		t.Errorf("Check = %v, want unknown", trust)
	}
}

func TestSaveCreatesTheDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "client", "nested")
	if err := Save(dir, List{}.Accept(Server{Address: "a:8080", Pin: "p"})); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, FileName)); err != nil {
		t.Fatalf("no file written: %v", err)
	}
}

// No directory is survivable, the way it is for the window profile: the client
// still opens, and nothing is remembered.
func TestLoadWithNoDirectoryIsEmpty(t *testing.T) {
	got, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Servers) != 0 {
		t.Error("servers appeared from nowhere")
	}
}

func TestSaveWithNoDirectoryIsAnError(t *testing.T) {
	if err := Save("", List{}); err == nil {
		t.Error("Save reported success with nowhere to write")
	}
}

// Forgetting has to survive a restart, or a mismatch could never be resolved.
func TestForgettingPersists(t *testing.T) {
	dir := t.TempDir()
	l := List{}.Accept(Server{Address: "a:8080", Pin: "one"})
	l = l.Accept(Server{Address: "b:8080", Pin: "two"})
	if err := Save(dir, l); err != nil {
		t.Fatalf("Save: %v", err)
	}

	l, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := Save(dir, l.Forget("a:8080")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := got.Find("a:8080"); ok {
		t.Error("the forgotten server came back after a restart")
	}
	if _, ok := got.Find("b:8080"); !ok {
		t.Error("the other server did not survive")
	}
}

// A half-written file must never be what Load sees.
func TestSaveLeavesNoTemporaryFileBehind(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, List{}.Accept(Server{Address: "a:8080", Pin: "p"})); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("%s left behind", e.Name())
		}
	}
}
