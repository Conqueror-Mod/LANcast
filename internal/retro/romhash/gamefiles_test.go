package romhash

import (
	"os"
	"path/filepath"
	"testing"
)

// A multi-disc game is its list, each disc, and each disc's tracks, with
// nothing outside its folder and nothing twice.
func TestGameFilesOfAnM3U(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "Game")
	hidden := filepath.Join(dir, ".hidden")
	if err := os.MkdirAll(hidden, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(parent, "outside.bin"), []byte("x"))
	write(t, filepath.Join(hidden, "D1.cue"), []byte("FILE \"D1 (Track 1).bin\" BINARY\nFILE \"..\\..\\outside.bin\" BINARY\n"))
	write(t, filepath.Join(hidden, "D2.cue"), []byte("FILE \"D2.bin\" BINARY\nFILE \"D2.bin\" BINARY\n"))
	m3u := filepath.Join(dir, "Game.m3u")
	write(t, m3u, []byte(".hidden/D1.cue\n.hidden/D2.cue\n"))

	got, err := GameFiles(m3u)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		m3u,
		filepath.Join(hidden, "D1.cue"),
		filepath.Join(hidden, "D1 (Track 1).bin"),
		filepath.Join(hidden, "D2.cue"),
		filepath.Join(hidden, "D2.bin"),
	}
	if len(got) != len(want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestGameFilesOfACartridgeIsItself(t *testing.T) {
	p := filepath.Join(t.TempDir(), "Game.z64")
	write(t, p, []byte("x"))
	got, err := GameFiles(p)
	if err != nil || len(got) != 1 || got[0] != p {
		t.Errorf("got %v, %v", got, err)
	}
}
