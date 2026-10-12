package lancast

import (
	"strings"
	"testing"
)

func TestTheNoticeIsEmbedded(t *testing.T) {
	if !strings.Contains(Notice, "GNU Affero General Public License") {
		t.Fatal("NOTICE was not embedded")
	}
}

func TestReleaseNotesByVersion(t *testing.T) {
	for _, v := range []string{"0.9.79", "v0.9.79", " v0.9.79 "} {
		notes, ok := ReleaseNotes(v)
		if !ok || !strings.Contains(notes, "LANcast.exe") {
			t.Fatalf("ReleaseNotes(%q) = %v, %q", v, ok, notes[:min(len(notes), 40)])
		}
	}
	for _, v := range []string{"", "dev", "0.0.0", "../NOTICE", "README"} {
		if _, ok := ReleaseNotes(v); ok {
			t.Fatalf("ReleaseNotes(%q) answered", v)
		}
	}
}
