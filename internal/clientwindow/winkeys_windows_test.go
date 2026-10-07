package clientwindow

import (
	"testing"
	"unsafe"
)

// SendInput is told the size of INPUT and refuses a wrong one without saying
// why, which would leave Start opening after every Win+arrow. The hook
// structure is read by pointer from what Windows hands over. Both pinned to the
// 64-bit layouts in winuser.h.
func TestKeyStructuresMatchWinuser(t *testing.T) {
	if got := unsafe.Sizeof(keyInput{}); got != 40 {
		t.Errorf("INPUT is %d bytes, want 40", got)
	}
	if got := unsafe.Sizeof(kbdLLHook{}); got != 24 {
		t.Errorf("KBDLLHOOKSTRUCT is %d bytes, want 24", got)
	}
	if got := unsafe.Offsetof(keyInput{}.Vk); got != 8 {
		t.Errorf("INPUT.ki.wVk at %d, want 8", got)
	}
}
