package playlist

import "testing"

// A line rooted with no drive belongs on the playlist's drive, not on the
// drive of the service's working directory.
func TestResolveRootedPathTakesThePlaylistsDrive(t *testing.T) {
	got, ok := Resolve(`D:\Lists`, `\Music\a.mp3`)
	if !ok || got != `D:\Music\a.mp3` {
		t.Errorf(`Resolve = %q, %v; want D:\Music\a.mp3`, got, ok)
	}
	// A line that names its own drive keeps it.
	if got, _ := Resolve(`D:\Lists`, `E:\Music\a.mp3`); got != `E:\Music\a.mp3` {
		t.Errorf(`Resolve = %q; want E:\Music\a.mp3`, got)
	}
}
