//go:build windows

package clientwindow

import "testing"

// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 is the pseudo-handle -4. Written
// as a bit pattern because a negative constant does not convert to uintptr,
// and a pattern one bit off names a different awareness mode entirely --
// V1, or unaware -- with nothing to say so but a stretched window.
func TestThePerMonitorV2ContextIsMinusFour(t *testing.T) {
	var minusFour int64 = -4
	if dpiAwarenessPerMonitorV2 != uintptr(minusFour) {
		t.Fatalf("context = %#x, want -4 (%#x)", dpiAwarenessPerMonitorV2, uintptr(minusFour))
	}
}

// Setting it twice must not be an error: a manifest or an earlier call may
// have set it already, and that is the state wanted.
func TestSettingDPIAwarenessTwiceIsFine(t *testing.T) {
	_ = SetDPIAware()
	if err := SetDPIAware(); err != nil {
		t.Errorf("second SetDPIAware = %v, want nil", err)
	}
}
