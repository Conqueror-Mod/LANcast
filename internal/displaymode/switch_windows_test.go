//go:build windows

package displaymode

import (
	"testing"
	"unsafe"
)

// DEVMODEW is 220 bytes. One field off and Windows reads the frequency out of
// the panning width, with no error to say so.
func TestDevModeLayout(t *testing.T) {
	var d devMode
	if s := unsafe.Sizeof(d); s != 220 {
		t.Fatalf("devMode is %d bytes, DEVMODEW is 220", s)
	}
	if o := unsafe.Offsetof(d.BitsPerPel); o != 168 {
		t.Errorf("BitsPerPel at %d, want 168", o)
	}
	if o := unsafe.Offsetof(d.DisplayFrequency); o != 184 {
		t.Errorf("DisplayFrequency at %d, want 184", o)
	}
}

// The current mode of the primary display reads back as something sane. Read
// only: nothing here changes a mode, so the suite can run on a desk in use.
func TestCurrentModeReads(t *testing.T) {
	// A nil device is "the display this thread is on".
	m, err := current(nil)
	if err != nil {
		t.Skipf("no display in this session: %v", err)
	}
	if m.Width <= 0 || m.Height <= 0 || m.Hz <= 0 {
		t.Fatalf("current mode %+v", m)
	}
	if len(offered(nil)) == 0 {
		t.Fatal("the display lists no progressive modes")
	}
}
